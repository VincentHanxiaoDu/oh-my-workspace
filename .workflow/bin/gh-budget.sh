#!/usr/bin/env bash
# Is there API budget left, and how much of it belongs to the work rather than to watching the work?
#
# WHY THIS EXISTS. The watches and the agents draw on ONE 5000-requests-per-hour allowance, and
# nothing reserved any of it for the agents. **An instrument that consumes the thing it measures**:
# the watch polls, the poll spends the budget the role needs to do its job, the role's own calls then
# fail, and the watch reports LOOKUP FAILED for polls that its own polling made impossible. A role
# said it exactly: *"my check failed and cost the watch its poll in the same window."*
#
# THE ARITHMETIC, MEASURED RATHER THAN GUESSED, by counting calls through a stub `gh` on a board with
# six open pull requests:
#
#   watch-prs.sh   one poll  ≈  2 + 3 per open PR   ≈ 20 calls
#   queue.sh       one run   ≈  8 + 4 per open PR   ≈ 32 calls
#   ------------------------------------------------------------
#   one role, one poll of both                      ≈ 52 calls
#
#   at 60s  × 3 roles →  9360 calls/hour   — 1.9× OVER the limit, before any agent does any work
#   at 300s × 3 roles →  1872 calls/hour   — 37% of budget, leaving the rest for the work
#
# **The prompts prescribed 60 seconds.** A product agent independently raised its own watch to 300s
# and said why; it was right, and the framework was wrong. The default is now 300 and the number
# above is why. Nothing on a review board moves on a sixty-second timescale.
#
# READING THE LIMIT IS FREE. `GET /rate_limit` is documented as not counting against the limit, which
# is what makes a reservation checkable rather than another thing to spend.
#
# BUT IT READS ONLY THE PRIMARY LIMIT, AND THE OUTAGES ARE SECONDARY (Issue #81). Measured live while
# `queue.sh` and every sweep were returning HTTP 403 `API rate limit exceeded`:
#
#   core     4896/5000  resets in 25m
#   graphql  4964/5000  resets in 41m
#
# Both essentially untouched. GitHub's SECONDARY limit — the burst/concurrency throttle — is reported
# by no endpoint at all, so a guard reading those counters sees a healthy budget and keeps polling
# straight through a total refusal. `check` answered `4841`, exit 0, while every REST call was 403.
# **The reserve was never reached because the number it watched never moved.**
#
# SO THE ONLY PLACE A SECONDARY LIMIT IS EVER VISIBLE IS THE 403 ITSELF, and this file remembers the
# ones its callers were handed: a watch whose poll is refused calls `note-failure` with the output,
# and every later `check` holds until the throttle is expected to have lifted.
#
# AND IT DOES NOT LIFT ON THE RESET CLOCK. A secondary limit clears with QUIET, not when
# `.resources.core.reset` arrives — so `reset-in` is the wrong signal to wait on and `hold-for`
# exists to answer with the right one.
#
# Usage: gh-budget.sh check [reserve]   # exit 0 if remaining > reserve, 1 if not, 2 if unknown
#        gh-budget.sh reset-in          # seconds until the PRIMARY core limit resets
#        gh-budget.sh note-failure <text>  # record a refusal; exit 0 if it was a rate limit, 1 if not
#        gh-budget.sh secondary         # exit 0 and print seconds left if a secondary limit is live
#        gh-budget.sh hold-for [floor]  # seconds a caller should stand down for, right signal per case
#        gh-budget.sh --self-test
set -euo pipefail

# THE RESERVE IS FOR THE WORK, NOT FOR THE WATCHING. Below it, watching stops and the role keeps
# what is left — because a role that cannot call the API cannot review, merge, or close anything,
# and a watch that is still polling while that is true has inverted its own purpose.
DEFAULT_RESERVE=${ADF_BUDGET_RESERVE:-1500}

# WHERE THE OBSERVED REFUSALS ARE REMEMBERED. One file, shared by every watch on this machine,
# because a secondary limit is an account-wide state and one watch seeing it is enough for all of
# them. Overridable so a test can drive this without touching the machine's real state.
STATE_DIR=${ADF_BUDGET_STATE_DIR:-${TMPDIR:-/tmp}}
SECONDARY_FILE="$STATE_DIR/adf-secondary-rate-limit"
# How long to stay quiet when the refusal carried no `Retry-After`. GitHub documents at least one
# minute for a secondary limit and recommends exponential backoff; two is the conservative side of
# that, and the cost of being wrong is one skipped poll rather than a deepened throttle.
DEFAULT_SECONDARY_COOLDOWN=${ADF_SECONDARY_COOLDOWN:-120}

case "${1:-}" in
  check|reset-in|note-failure|secondary|hold-for|--self-test) : ;;
  *) echo "::error::usage: gh-budget.sh check [reserve] | reset-in | note-failure <text> | secondary | hold-for [floor] | --self-test" >&2; exit 2 ;;
esac

read_budget() { # -> "<remaining> <reset-epoch>" or empty
  gh api rate_limit --jq '"\(.resources.core.remaining) \(.resources.core.reset)"' 2>/dev/null || echo ""
}

# IS THIS TEXT A REFUSAL FOR RATE REASONS? Matched on the bodies GitHub actually sends and `gh`
# prints verbatim. A secondary limit says `You have exceeded a secondary rate limit`; a plain 403
# says `API rate limit exceeded`; the older wording is `abuse detection mechanism`. All three are
# the same instruction — stop calling — and all three are invisible in `/rate_limit`.
is_rate_refusal() {
  case "$1" in
    *"secondary rate limit"*|*"Secondary rate limit"*|*"SECONDARY RATE LIMIT"*) return 0 ;;
    *"API rate limit exceeded"*|*"api rate limit exceeded"*) return 0 ;;
    *"abuse detection"*|*"exceeded a rate limit"*) return 0 ;;
  esac
  return 1
}

# A `Retry-After` is GitHub telling you exactly how long to be quiet. Take it when it is there.
retry_after_from() {
  local s
  s=$(printf '%s' "$1" | grep -oiE 'retry[-_ ]?after"?:?[[:space:]]*"?[0-9]+' | grep -oE '[0-9]+' | head -1 || true)
  case "$s" in ''|*[!0-9]*) echo "$DEFAULT_SECONDARY_COOLDOWN" ;; *) echo "$s" ;; esac
}

secondary_left() { # -> seconds remaining on a live observation; exit 1 if there is none
  local line until now
  [ -f "$SECONDARY_FILE" ] || return 1
  line=$(cat "$SECONDARY_FILE" 2>/dev/null || echo "")
  until=${line%% *}
  case "$until" in ''|*[!0-9]*) return 1 ;; esac
  now=$(date +%s)
  [ "$until" -gt "$now" ] || return 1
  echo $(( until - now ))
}

secondary_message() { # $1 = seconds left
  echo "SECONDARY RATE LIMIT — GitHub is refusing calls with a secondary (burst/concurrency) throttle. This does NOT appear in the primary quota, which reads healthy throughout it. Standing down for ${1}s: a secondary limit clears with quiet, not on the reset clock, so polling through it extends it."
}

note_failure() { # record a refusal; exit 0 if it was a rate limit, 1 if it was some other outage
  local text=${1:-} secs until
  is_rate_refusal "$text" || return 1
  secs=$(retry_after_from "$text")
  until=$(( $(date +%s) + secs ))
  mkdir -p "$STATE_DIR" 2>/dev/null || true
  printf '%s %s\n' "$until" "$(date +%s)" > "$SECONDARY_FILE" 2>/dev/null || true
  echo "$secs"
  return 0
}

do_check() {
  local reserve=${1:-$DEFAULT_RESERVE} b rem reset left
  # THE SECONDARY LIMIT IS ASKED FIRST, because the primary answer is worthless while one is live:
  # 4896/5000 remaining was measured during an outage in which every call was refused. Answering
  # with that number would be answering a question nobody asked.
  if left=$(secondary_left); then
    secondary_message "$left"
    return 1
  fi
  b=$(read_budget)
  # UNKNOWN IS ITS OWN ANSWER AND IT IS NOT "PLENTY". If the budget cannot be read, saying yes would
  # spend what may not be there and saying no would stop a watch for a reason that may not exist.
  # Exit 2, and the caller decides — every caller here treats it as "poll, but say it was unknown".
  if [ -z "$b" ]; then
    echo "BUDGET UNKNOWN — could not read the rate limit. This is a LOOKUP FAILURE and NOT a statement that there is budget."
    return 2
  fi
  rem=${b% *}; reset=${b#* }
  if [ "$rem" -gt "$reserve" ]; then
    # AN UNDETERMINED ANSWER MUST NOT WEAR A DETERMINED FACE. This number is true about the PRIMARY
    # hourly quota and about nothing else. The secondary throttle cannot be read from any endpoint,
    # so all that is known here is that none has been observed — which is not the same as knowing
    # there is none, and printing a bare number said otherwise for the whole of the outage.
    echo "$rem on the primary core quota; no secondary (burst) limit observed. The secondary limit CANNOT BE DETERMINED without spending a call, so this is a statement about the primary quota only and NOT a promise that the next call will be answered."
    return 0
  fi
  local in=$(( reset - $(date +%s) ))
  [ "$in" -lt 0 ] && in=0
  echo "$rem left, at or below the reserve of $reserve; resets in ${in}s. Not polling: the remaining budget is reserved for this role's own work."
  return 1
}

# HOW LONG TO STAND DOWN, ANSWERED PER CAUSE. A secondary limit clears with quiet and its remaining
# cooldown is the only meaningful number; a primary exhaustion clears on the reset clock. Answering
# both with `reset-in` waits on the wrong signal for the case that actually happens.
do_hold_for() {
  local floor=${1:-60} s
  if s=$(secondary_left); then
    :
  else
    s=$(read_budget)
    if [ -z "$s" ]; then s=$floor; else s=$(( ${s#* } - $(date +%s) )); fi
  fi
  case "$s" in ''|*[!0-9-]*) s=$floor ;; esac
  [ "$s" -lt "$floor" ] && s=$floor
  [ "$s" -gt 600 ] && s=600
  echo "$s"
}

self_test() {
  local rc=0 tmp out
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' RETURN
  cp "${BASH_SOURCE[0]}" "$tmp/gh-budget.sh"

  # 1. PLENTY OF BUDGET PASSES.
  printf '#!/usr/bin/env bash\necho "4900 %s"\n' "$(( $(date +%s) + 600 ))" > "$tmp/gh"; chmod +x "$tmp/gh"
  ( PATH="$tmp:$PATH" bash "$tmp/gh-budget.sh" check 1500 ) >/dev/null 2>&1 \
    || { echo "SELF-TEST FAIL: 4900 remaining against a reserve of 1500 was reported as exhausted" >&2; rc=1; }

  # 2. BELOW THE RESERVE FAILS, AND SAYS WHEN IT WILL RECOVER. A stop with no recovery time is a
  #    stop a role cannot plan around.
  printf '#!/usr/bin/env bash\necho "900 %s"\n' "$(( $(date +%s) + 300 ))" > "$tmp/gh"
  out=$( PATH="$tmp:$PATH" bash "$tmp/gh-budget.sh" check 1500 2>&1 ) \
    && { echo "SELF-TEST FAIL: 900 remaining against a reserve of 1500 PASSED" >&2; rc=1; }
  case "$out" in
    *"resets in"*) : ;;
    *) echo "SELF-TEST FAIL: being below the reserve did not say when it recovers (got: $out)" >&2; rc=1 ;;
  esac

  # 3. AN UNREADABLE LIMIT IS NEITHER. **This is the one that matters.** Reporting "no budget" would
  #    stop every watch on an unrelated outage; reporting "budget" would spend what may not exist.
  #    It is exit 2, distinct from both, and it says LOOKUP FAILURE in the words this project uses.
  printf '#!/usr/bin/env bash\nexit 1\n' > "$tmp/gh"
  local urc=0
  out=$( PATH="$tmp:$PATH" bash "$tmp/gh-budget.sh" check 1500 2>&1 ) || urc=$?
  [ "$urc" -eq 2 ] || { echo "SELF-TEST FAIL: an unreadable rate limit exited $urc, not 2 — it must not share an answer with 'plenty' or with 'exhausted'" >&2; rc=1; }
  case "$out" in
    *"LOOKUP FAILURE"*) : ;;
    *) echo "SELF-TEST FAIL: an unreadable limit was not named as a lookup failure (got: $out)" >&2; rc=1 ;;
  esac

  # 4. A SECONDARY RATE LIMIT IS NOT A HEALTHY BUDGET. **This is Issue #81 and it is the arm whose
  #    absence shipped the defect**: arms 1-3 drive 4900-vs-1500, 900-vs-1500 and an unreadable
  #    limit, all of them PRIMARY, which is exactly why a guard that reads only the primary limit
  #    passed every one of them.
  #
  #    The stub reproduces the measured outage: `/rate_limit` answers 4896/5000 — nearly untouched,
  #    the real number from the day — while the calls the watch actually makes are being refused
  #    with a 403. `check` must HOLD, must name the throttle as a burst limit, and must say when it
  #    will retry.
  local sdir; sdir=$(mktemp -d)
  printf '#!/usr/bin/env bash\necho "4896 %s"\n' "$(( $(date +%s) + 1500 ))" > "$tmp/gh"; chmod +x "$tmp/gh"
  out=$( ADF_BUDGET_STATE_DIR="$sdir" PATH="$tmp:$PATH" bash "$tmp/gh-budget.sh" check 1500 2>&1 ) \
    || { echo "SELF-TEST FAIL: 4896 primary remaining with no observed refusal was reported as exhausted" >&2; rc=1; }
  case "$out" in
    *"CANNOT BE DETERMINED"*) : ;;
    *) echo "SELF-TEST FAIL: a healthy primary quota was reported without saying the secondary limit is undetermined — an undetermined answer wearing a determined face (got: $out)" >&2; rc=1 ;;
  esac
  # Now hand it the refusal, exactly as a watch whose poll was 403'd does.
  ADF_BUDGET_STATE_DIR="$sdir" bash "$tmp/gh-budget.sh" note-failure \
    "gh: You have exceeded a secondary rate limit and have been temporarily blocked from content creation. (HTTP 403)" >/dev/null \
    || { echo "SELF-TEST FAIL: a 403 naming a secondary rate limit was not recognised as one" >&2; rc=1; }
  local src=0
  out=$( ADF_BUDGET_STATE_DIR="$sdir" PATH="$tmp:$PATH" bash "$tmp/gh-budget.sh" check 1500 2>&1 ) || src=$?
  [ "$src" -eq 1 ] || { echo "SELF-TEST FAIL: a live secondary rate limit exited $src, not 1 — the watch keeps polling straight through the outage (out: $out)" >&2; rc=1; }
  case "$out" in
    *"secondary"*|*"SECONDARY"*) : ;;
    *) echo "SELF-TEST FAIL: holding on a secondary limit did not name it as one (got: $out)" >&2; rc=1 ;;
  esac
  case "$out" in
    *"burst"*) : ;;
    *) echo "SELF-TEST FAIL: the hold did not say it is a burst throttle, so a role cannot tell it from primary exhaustion (got: $out)" >&2; rc=1 ;;
  esac
  case "$out" in
    *"Standing down for"*) : ;;
    *) echo "SELF-TEST FAIL: the hold did not say when it retries — a stop with no recovery time is one a role cannot plan around (got: $out)" >&2; rc=1 ;;
  esac
  # AND THE WAIT MUST BE THE SECONDARY'S COOLDOWN, NOT THE PRIMARY'S RESET. The stub's core limit
  # resets in 1500s; a secondary limit clears with quiet and has nothing to do with that clock.
  local hf; hf=$( ADF_BUDGET_STATE_DIR="$sdir" PATH="$tmp:$PATH" bash "$tmp/gh-budget.sh" hold-for 60 )
  [ "$hf" -le 600 ] && [ "$hf" -gt 0 ] \
    || { echo "SELF-TEST FAIL: hold-for answered '$hf' under a secondary limit — it is waiting on the primary reset clock, which is the wrong signal" >&2; rc=1; }

  # 5. AN OUTAGE THAT IS NOT A RATE LIMIT MUST NOT BE RECORDED AS ONE. `HOLDING` and `LOOKUP FAILED`
  #    are different states and a network timeout is the second one; recording it here would put
  #    every watch to sleep for an outage that quiet does not fix.
  ADF_BUDGET_STATE_DIR="$sdir/other" bash "$tmp/gh-budget.sh" note-failure "gh: dial tcp 140.82.116.6:443: operation timed out" >/dev/null 2>&1 \
    && { echo "SELF-TEST FAIL: a dial timeout was recorded as a secondary rate limit — an outage would be spelled the same way as a throttle" >&2; rc=1; }
  rm -rf "$sdir"

  [ "$rc" -eq 0 ] && echo "self-test passed: headroom passes and says what it did not determine, below-reserve fails and says when it recovers, an unreadable limit is neither, an observed secondary limit holds and names itself, and a network outage is not recorded as a throttle"
  return $rc
}

[ "$1" = "--self-test" ] && { self_test; exit $?; }
[ "$1" = "note-failure" ] && { note_failure "${2:-}" && exit 0 || exit 1; }
[ "$1" = "secondary" ] && { left=$(secondary_left) || exit 1; echo "$left"; exit 0; }
[ "$1" = "hold-for" ] && { do_hold_for "${2:-60}"; exit 0; }
[ "$1" = "reset-in" ] && {
  b=$(read_budget)
  [ -n "$b" ] || { echo 60; exit 2; }
  in=$(( ${b#* } - $(date +%s) )); [ "$in" -lt 0 ] && in=0
  echo "$in"; exit 0
}
do_check "${2:-}"
