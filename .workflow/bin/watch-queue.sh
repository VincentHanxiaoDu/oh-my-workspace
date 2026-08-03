#!/usr/bin/env bash
# Emit one line every time NEW work appears in a role's queue. Feed this to a monitor and the role
# is woken by its work instead of waiting to be told about it.
#
# WHY THIS EXISTS. A role that reads its queue once works one round and stops. Somebody then has to
# notice that new work arrived and start it again — which is a coordinator, which is the bottleneck
# this process is built to remove. The queue is a state the role can watch.
#
# WHAT IT EMITS, and the second one matters as much as the first:
#
#   NEW  #42  feat(x): ...        an item that was not in the queue on the previous poll
#   LOOKUP FAILED: <reason>       the poll could not be answered
#   WATCHING <role> — poll #k     the watch is ALIVE and was answered
#   WATCH DIED (exit n)           it has stopped, with the number that explains it
#
# **Silence is not success.** If this only emitted new work, an expired token or an exhausted API
# quota would look exactly like a quiet queue, and a role would sit idle believing it was done. So a
# failed poll is an event too. It does not kill the watch — a transient outage should wake you, not
# end the loop.
#
# **AND SILENCE IS NOT ALIVENESS EITHER.** A watch that emits only on change is indistinguishable
# from a watch that has stopped existing — which is not hypothetical here: the sibling watcher died
# repeatedly and the role it served sat idle believing its queue was empty. So this one says it is
# still standing, sparsely, and says it has died, loudly.
#
# Usage: watch-queue.sh <role> [interval-seconds]     default interval: 300
#        watch-queue.sh --self-test
set -euo pipefail

# THE SAME PAIR AS watch-prs.sh, FOR THE SAME REASON — see the long note there. A death is announced
# with the exit code that explains it, and SIGURG is ignored because the observed deaths carried
# exit 144 (`128 + 16`) and a preemption signal from a Go parent must not be able to end a watch.
# INT and TERM stay untrapped: whoever starts this must be able to stop it.
trap 'wrc=$?; [ "$wrc" -eq 0 ] || echo "WATCH DIED (exit $wrc) — this role is now BLIND, and a dead watch looks exactly like an empty queue. Restart it, and read your queue directly with .workflow/bin/queue.sh ${1:-<role>} before assuming there is nothing to do." >&2' EXIT
trap '' URG

case "${1:-}" in
  -*) [ "$1" = "--self-test" ] || {
        echo "::error::unknown option '$1'. This is a typo, not an argument — refusing." >&2; exit 2; } ;;
esac

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# THE REASON A POLL FAILED IS AT THE END OF ITS OUTPUT, NEVER AT THE START (Issue #64).
#
# `queue.sh` is captured with `2>&1`, so what comes back is EVERYTHING it printed before it died —
# headings, work items, then the failure. Keeping the first 200 characters therefore keeps the
# headings and throws away the only part that says what went wrong. Measured, with a stub that
# prints a normal queue and then fails the way the real one did:
#
#   head   LOOKUP FAILED: FEATURES WHOSE WORK HAS LANDED — UAT on main and CLOSE   #9  feat(notes):
#          draft notes into the outbox and publish them   #10 feat(publish): three publication modes…
#   tail   LOOKUP FAILED: …#38 feat(outbox): drafts and modes gh: dial tcp 140.82.116.6:443:
#          operation timed out
#
# Same bytes, same budget. One of them names the outage. The first was read as a misfire and a round
# of a release day was spent proving the watch had been right all along.
#
# A SHORT REASON IS STILL SHOWN WHOLE, byte for byte as before. A fix that always printed a tail
# would mangle the common case, where the entire output is the reason and there is nothing to trim.
reason() { # reason <text> <budget>
  local flat budget=$2
  flat=$(printf '%s' "$1" | tr '\n' ' ')
  if [ "${#flat}" -le "$budget" ]; then printf '%s' "$flat"; return 0; fi
  # The ellipsis is inside the budget, so the line length is unchanged and the reader is told that
  # something was dropped — an elided reason must not read like a complete one.
  printf '…%s' "${flat: -$((budget - 1))}"
}

self_test() {
  local rc=0
  # THE FAILURE PATH MUST EMIT, AND MUST NOT END THE WATCH — driven against a queue that always
  # fails, not asserted by grepping the source.
  #
  # The first version of this arm was `grep -q 'LOOKUP FAILED' "$BASH_SOURCE"`. It passed with the
  # emit deleted, because the string also appears in this file's header comment: **the check was
  # matching its own documentation.** A mutation run is what found that; reading it would not have.
  local ftmp fout n_fail
  ftmp=$(mktemp -d)
  printf '#!/usr/bin/env bash\necho "boom" >&2\nexit 1\n' > "$ftmp/queue.sh"; chmod +x "$ftmp/queue.sh"
  cp "${BASH_SOURCE[0]}" "$ftmp/watch-queue.sh"
  ( bash "$ftmp/watch-queue.sh" dev 1 >"$ftmp/out" 2>&1 & echo $! > "$ftmp/pid" )
  # WAITS FOR THE SECOND EMIT RATHER THAN SLEEPING THREE SECONDS AND HOPING. Same race, same fix and
  # same reason as the announce-once arm below: on a busy machine one poll can outlast the whole
  # window, and this arm then reported that a working watch had stopped after one failure. A red
  # that means nothing costs a role a round to disprove.
  local fwaited=0
  while [ "$fwaited" -lt 30 ]; do
    [ "$(grep -c '^LOOKUP FAILED' "$ftmp/out" 2>/dev/null || echo 0)" -ge 2 ] && break
    sleep 1; fwaited=$((fwaited + 1))
  done
  kill "$(cat "$ftmp/pid")" 2>/dev/null || true
  fout=$(cat "$ftmp/out" 2>/dev/null || echo "")
  n_fail=$(printf '%s\n' "$fout" | grep -c '^LOOKUP FAILED' || true)
  rm -rf "$ftmp"
  [ "$n_fail" -ge 1 ] \
    || { echo "SELF-TEST FAIL: a failing poll emitted nothing — an outage is indistinguishable from an empty queue" >&2; rc=1; }
  # TWO of them: one proves it emits, two prove it did not exit after the first.
  [ "$n_fail" -ge 2 ] \
    || { echo "SELF-TEST FAIL: a failing poll emitted once and stopped — a transient outage must wake the role, not end the watch (got $n_fail)" >&2; rc=1; }

  # THE REASON MUST SURVIVE A LONG OUTPUT (Issue #64). The arm above drives a stub whose entire
  # output is `boom` — under the budget, so it could never observe the truncation, which is exactly
  # why this shipped and stayed shipped. This one prints a normal-looking queue FIRST and fails
  # after it, which is what `2>&1` on a real `queue.sh` produces.
  local ltmp lout
  ltmp=$(mktemp -d)
  cat > "$ltmp/queue.sh" <<'STUB'
#!/usr/bin/env bash
# A queue that answers at length and then dies, the way the real one did.
echo "FEATURES WHOSE WORK HAS LANDED — UAT on main and CLOSE"
for i in $(seq 1 12); do echo "  #$i  feat(notes): draft notes into the outbox and publish them"; done
echo "gh: dial tcp 140.82.116.6:443: operation timed out" >&2
exit 1
STUB
  chmod +x "$ltmp/queue.sh"
  cp "${BASH_SOURCE[0]}" "$ltmp/watch-queue.sh"
  ( bash "$ltmp/watch-queue.sh" dev 1 >"$ltmp/out" 2>&1 & echo $! > "$ltmp/pid" )
  # WAITS FOR THE OUTCOME INSTEAD OF SLEEPING FOR A GUESS. Measured: --self-test failed once and
  # then passed six consecutive times, and the role that hit the red spent a round establishing that
  # its own guard was flaky rather than that anything was wrong. A poll can outlast a fixed window on
  # a busy machine; a guard that fails at random is close to no guard at all, because every red it
  # produces has to be investigated and most of them mean nothing. This still fails in bounded time
  # when the behaviour is genuinely broken.
  local lwaited=0
  while [ "$lwaited" -lt 30 ]; do
    grep -q "operation timed out" "$ltmp/out" 2>/dev/null && break
    sleep 1; lwaited=$((lwaited + 1))
  done
  kill "$(cat "$ltmp/pid")" 2>/dev/null || true
  lout=$(cat "$ltmp/out" 2>/dev/null || echo "")
  rm -rf "$ltmp"
  case "$lout" in
    *"operation timed out"*) : ;;
    *) echo "SELF-TEST FAIL: a poll that printed a long queue and THEN failed reported the queue and not the failure — the reason is at the end and the head was kept (got: $lout)" >&2; rc=1 ;;
  esac

  # AND A SHORT REASON IS STILL SHOWN WHOLE. The `boom` arm above already drives this path; here the
  # rendering is asserted byte for byte, because a fix that always printed a tail would put an
  # ellipsis on output that was never truncated.
  local stmp sout
  stmp=$(mktemp -d)
  printf '#!/usr/bin/env bash\necho "boom" >&2\nexit 1\n' > "$stmp/queue.sh"; chmod +x "$stmp/queue.sh"
  cp "${BASH_SOURCE[0]}" "$stmp/watch-queue.sh"
  ( bash "$stmp/watch-queue.sh" dev 1 >"$stmp/out" 2>&1 & echo $! > "$stmp/pid" )
  sleep 2
  kill "$(cat "$stmp/pid")" 2>/dev/null || true
  sout=$(grep -m1 '^LOOKUP FAILED' "$stmp/out" 2>/dev/null || echo "")
  rm -rf "$stmp"
  [ "$sout" = "LOOKUP FAILED: boom" ] \
    || { echo "SELF-TEST FAIL: a short reason was not shown byte-identically — expected 'LOOKUP FAILED: boom', got '$sout'" >&2; rc=1; }

  # An unknown role must refuse rather than watch an empty queue forever.
  ( REPO=x/y bash "${BASH_SOURCE[0]}" not-a-role 1 ) >/dev/null 2>&1 \
    && { echo "SELF-TEST FAIL: an unknown role was accepted" >&2; rc=1; }

  # THE DEDUPLICATION MUST ACTUALLY DEDUPLICATE: an item already announced must not be announced
  # again, or the monitor re-sends the whole queue on every poll and buries the role in
  # notifications for work it already has.
  #
  # DRIVEN THROUGH THE REAL SCRIPT, against a stub queue that grows between polls. The first
  # version of this arm re-implemented the emit loop twelve lines away and asserted the copy — and
  # the copy was wrong in a way the real code is not: it ran the loop in a pipeline, where `seen`
  # is a subshell variable that does not survive, while the real loop uses process substitution and
  # keeps it. **The mirror failed and the original was fine**, which is the harmless direction of a
  # defect that usually runs the other way.
  local tmp out n
  tmp=$(mktemp -d)
  # THE COUNTER LIVES IN THIS RUN'S OWN DIRECTORY. It was a fixed path in TMPDIR, so two self-tests
  # running at once — which `make ci` and a role checking the same script both do — shared one
  # counter and each saw the other's polls.
  cat > "$tmp/queue.sh" <<'STUB'
#!/usr/bin/env bash
# Stub: two items on the first call, three on every call after.
n_file="$(dirname "$0")/wq-selftest-count"
c=$(cat "$n_file" 2>/dev/null || echo 0); echo $((c+1)) > "$n_file"
printf '  #1  a\n  #2  b\n'
[ "$c" -ge 1 ] && printf '  #3  c\n'
exit 0
STUB
  chmod +x "$tmp/queue.sh"
  cp "${BASH_SOURCE[0]}" "$tmp/watch-queue.sh"
  # WAIT FOR THE OUTCOME, DO NOT SLEEP FOR A GUESS.
  #
  # This arm slept three seconds and asserted on whatever had arrived. That is a race against the
  # machine, and it lost: measured on a live board, `--self-test` failed once and then passed six
  # consecutive times, and the role that hit the red spent a round establishing that its own guard
  # was flaky rather than that anything was wrong. **A guard that fails at random is close to no
  # guard at all** — every red it produces has to be investigated and most of them mean nothing.
  #
  # Each poll calls `gh-budget.sh check`, which can touch the network, so one poll can take longer
  # than the whole window. Waiting for the condition with a generous ceiling still fails in bounded
  # time when the emit loop is genuinely broken — it just stops failing when the machine is busy.
  # KILLED PORTABLY: `timeout` is GNU coreutils and is absent on a stock macOS, where this ran and
  # reported `timeout: command not found` — correctly failing rather than passing, which is the only
  # reason it was noticed.
  ( REPO=x/y bash "$tmp/watch-queue.sh" dev 1 >"$tmp/out" 2>&1 & echo $! > "$tmp/pid" )
  local waited=0
  while [ "$waited" -lt 30 ]; do
    [ "$(grep -c '^NEW ' "$tmp/out" 2>/dev/null || echo 0)" -ge 3 ] && break
    sleep 1; waited=$((waited + 1))
  done
  kill "$(cat "$tmp/pid")" 2>/dev/null || true
  out=$(cat "$tmp/out" 2>/dev/null || echo "")
  rm -rf "$tmp"
  n=$(printf '%s\n' "$out" | grep -c '^NEW ' || true)
  [ "$n" -eq 3 ] \
    || { echo "SELF-TEST FAIL: expected 3 NEW lines across repeated polls of a queue holding 3 distinct items, got $n: $out" >&2; rc=1; }

  # THE HEARTBEAT MUST ARRIVE WHILE THE WATCH IS ALIVE. Asserted on the run above, which polled an
  # empty-of-new-work queue repeatedly — exactly the condition under which the old build said
  # nothing at all and a dead watch was indistinguishable from a quiet one.
  case "$out" in
    *WATCHING*) : ;;
    *) echo "SELF-TEST FAIL: a live watch emitted no WATCHING line — silence would again mean both 'no work' and 'dead'" >&2; rc=1 ;;
  esac

  # AND A DEATH MUST ANNOUNCE ITSELF, driven by an exit the loop cannot swallow: an unknown role,
  # which refuses with 2 before the loop is entered.
  local dout
  dout=$( bash "${BASH_SOURCE[0]}" not-a-role 1 2>&1 || true )
  case "$dout" in
    *"WATCH DIED"*) : ;;
    *) echo "SELF-TEST FAIL: the watch exited non-zero without saying so — a silent death is the state this file exists to remove (got: $dout)" >&2; rc=1 ;;
  esac

  [ "$rc" -eq 0 ] && echo "self-test passed: a failed poll emits and does not end the watch, unknown roles refuse, an item is announced once, the heartbeat arrives, and a death announces itself"
  return $rc
}

[ "${1:-}" = "--self-test" ] && { self_test; exit $?; }

role=${1:?usage: watch-queue.sh <role> [interval-seconds] | --self-test}
# 300s, NOT 60. Measured on a six-pull-request board: one poll of both watches costs a role about 52
# API calls, so three roles at 60s is ~9360 calls/hour against a limit of 5000 — 1.9x over, before
# any agent does any work of its own. At 300s it is ~1872, about 37% of the budget. A product agent
# raised its own watch to 300 unilaterally and said why; it was right and this default was wrong.
# Nothing on a review board moves on a sixty-second timescale.
interval=${2:-300}

case "$role" in
  dev|qa|product|ops|pm) : ;;
  *) echo "::error::'$role' is not a role. One of: dev qa product ops pm" >&2; exit 2 ;;
esac

# Announce what is already there on the first poll, so a role that starts the watch before reading
# its queue is not left waiting for a change that has already happened.
seen=""
polls=0

here_bin=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# Sleep until the throttle that is actually holding this watch is expected to lift, but never less
# than one interval and never more than ten minutes — an unreadable reset must not park the watch
# indefinitely.
#
# `hold-for`, NOT `reset-in`: the two causes clear on different clocks. A PRIMARY exhaustion clears
# when the hourly quota resets. A SECONDARY (burst) limit clears with QUIET and has nothing to do
# with that reset — waiting on `reset-in` there is waiting on a signal that never described the
# problem (Issue #81).
HOLD_SLEEP() { "$here_bin/gh-budget.sh" hold-for "$interval" 2>/dev/null || echo "$interval"; }

while true; do
  # THE WORK COMES BEFORE THE WATCHING. Below the reserve this watch stops polling and waits for the
  # limit to reset, because a role that cannot call the API cannot review, merge or close anything —
  # and a watch still spending budget while that is true has inverted its own purpose. Measured: a
  # role reported "my check failed and cost the watch its poll in the same window."
  #
  # HOLDING IS A THIRD STATE. Not a failed lookup, not a quiet board — it says the watch is alive,
  # deliberately idle, and when it resumes. Reading the limit is free and does not spend it.
  bmsg=$("$here_bin/gh-budget.sh" check 2>&1) && bok=0 || bok=$?
  if [ "${bok:-0}" -eq 1 ]; then
    echo "HOLDING — $bmsg"
    sleep "$(HOLD_SLEEP)"
    continue
  fi
  out=""
  if ! out=$("$here/queue.sh" "$role" 2>&1); then
    # A REFUSED CALL IS THE ONLY PLACE A SECONDARY RATE LIMIT IS EVER VISIBLE (Issue #81). The budget
    # guard above reads `/rate_limit`, which reports the PRIMARY hourly quota and nothing else —
    # measured at 4896/5000 while every one of these polls was coming back 403. So the 403 is handed
    # to the guard here, and it becomes the reason the NEXT check holds instead of waving the watch
    # through. Reporting this as `LOOKUP FAILED` and polling again on the interval is what turned a
    # throttle into a self-sustaining outage: each retry renews the burst that caused it.
    if hold=$("$here_bin/gh-budget.sh" note-failure "$out" 2>/dev/null); then
      echo "HOLDING — GitHub refused this poll with a SECONDARY (burst/concurrency) rate limit, which does NOT appear in the primary quota. Standing down for about ${hold}s; a secondary limit clears with quiet, not on the reset clock, so retrying now extends it."
      sleep "$(HOLD_SLEEP)"
      continue
    fi
    # A poll that could not be answered is an EVENT, not silence — and it is a DIFFERENT event from
    # the hold above. A dial timeout is not a throttle and quiet does not fix it.
    echo "LOOKUP FAILED: $(reason "$out" 200)"
    sleep "$interval"
    continue
  fi

  # Queue items are the indented `  #<n>  <title>` lines; headings and counts are not work.
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    case "$seen" in *"|$line|"*) continue ;; esac
    seen="$seen|$line|"
    echo "NEW $line"
  done < <(printf '%s\n' "$out" | grep -E '^  #[0-9]+' || true)

  # THE HEARTBEAT: first poll, then every tenth. The first makes starting the watch observable at
  # all; the rest make its absence a diagnosis instead of a guess. Sparse on purpose — a heartbeat
  # that arrives every minute is scrolled past, and one that is scrolled past cannot be missed.
  polls=$((polls + 1))
  if [ "$polls" -eq 1 ] || [ $((polls % 10)) -eq 0 ]; then
    echo "WATCHING  $role  —  poll #$polls, queue read, nothing new. If this line stops arriving, this watch is DEAD: restart it and read '.workflow/bin/queue.sh $role' yourself."
  fi

  sleep "$interval"
done
