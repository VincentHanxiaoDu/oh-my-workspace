#!/usr/bin/env bash
# What this role does next. Derived from repository state alone — never from being told.
#
# PRD R3: an agent must be able to compute its own queue. There is no owner:* label, no assignment
# message, no coordinator-maintained list. State is stored once.
#
# Usage: queue.sh <role>          # dev | qa | product | ops | pm | owner
#        queue.sh --self-test
set -euo pipefail

case "${1:-}" in
  -*)
    # An unknown flag is a typo, not data. A one-letter slip (`--self-tests`) must not be taken as
    # a positional argument and silently change what this checks.
    [ "$1" = "--self-test" ] || {
      echo "::error::unknown option '$1'. This is a typo, not an argument — refusing." >&2; exit 2; }
    ;;
esac

# THE OUTPUT OF A FAILED LOOKUP IS NOT AN EMPTY QUEUE. PRD's unifying defect: `could not see`
# rendering as `nothing to see`. Every query below either returns data or this script exits
# non-zero. An agent that gets rc=0 and no items may conclude it has no work; that conclusion must
# only ever be reachable from a query that actually ran.
api() {
  local out rc=0
  out=$(gh api "$@" 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "::error::the queue could not be read: $out" >&2
    echo "  This is a LOOKUP FAILURE and NOT a statement that you have no work. Do not proceed as" >&2
    echo "  though the queue were empty. Retry, or report the outage — REST works when GraphQL's" >&2
    echo "  quota is exhausted, which is a real and observed condition." >&2
    exit 1
  fi
  printf '%s' "$out"
}

# RESOLVED WHEN A QUERY IS ABOUT TO RUN, NOT AT LOAD TIME. The first version resolved it at the
# top of the file, so `--self-test` — which touches no network — could not run outside a checkout
# and reported `no repository`. A self-test that cannot run is not a failing self-test, and the two
# must never share an exit path. Caught by running it.
resolve_repo() {
  [ -n "${REPO:-}" ] && return 0
  # FROM THE GIT REMOTE, NOT FROM THE API. `gh repo view` is a GraphQL call, and GraphQL has its
  # own quota: measured exhausted (5000/5000) on a working day while REST still had headroom. With
  # the API version, every role's queue became "no repository" the moment that quota ran out —
  # an outage in one subsystem silently disabling the thing that tells every agent what to do.
  # The remote URL is already on disk and answers the same question.
  local url
  url=$(git config --get remote.origin.url 2>/dev/null || echo "")
  if [ -n "$url" ]; then
    REPO=$(printf '%s' "$url" | sed -E 's#^(https://[^/]+/|git@[^:]+:)##; s#\.git$##')
  fi
  # Only then the API, for a checkout with no origin.
  [ -n "${REPO:-}" ] || REPO=$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || echo "")
  [ -n "${REPO:-}" ] || { echo "::error::no repository: this checkout has no 'origin' remote and the API could not be asked. Set REPO." >&2; exit 2; }
}

# REST, not GraphQL: the shared GraphQL quota was measured exhausted (5000/5000) while REST still
# had headroom, and `gh issue list` returned an EMPTY FILE rather than an error when that happened.
issues() { resolve_repo; api --paginate "repos/$REPO/issues?state=open&per_page=100"; }

# EVERY FILTER NAMES WHAT IT DROPPED. The "named, not counted" fix was applied to one filter and
# not the other three, so on the arm where silent suppression was REPORTED it became more silent
# than the parenthesis that was filed — no count, no name, no trace. A filter that removes work
# without saying so is the defect this queue exists to prevent, and it recurred inside its own fix.
drop() { # drop <heading-var> <numbers> <why>  -> prints what it removed
  local nums=$2 why=$3 hidden
  # NOTHING TO DROP IS A NO-OP, NOT A PRINT. This said `printf '%s' "$out"` — a leftover from a
  # version where this function returned the list — so on every queue with nothing filtered it
  # printed the whole list to stdout ABOVE the heading. The lines then read as the role's work when
  # they were the opposite, and two agents filed it before I saw it.
  [ -n "$nums" ] || return 0
  hidden=$(printf '%s\n' "$out" | grep -E "^  #($(printf '%s' "$nums" | tr '\n' '|' | sed 's/|$//'))  " || true)
  out=$(printf '%s\n' "$out" | grep -vE "^  #($(printf '%s' "$nums" | tr '\n' '|' | sed 's/|$//'))  " || true)
  [ -z "$hidden" ] || DROPPED="$DROPPED
  ($why)
$(printf '%s' "$hidden" | sed 's/^  /      /')"
}

emit() { # emit <heading> <jq-filter> [--unclaimed|--unbuilt|--landed|--unruled]
  local head=$1 filter=$2 skip=${3:-} out DROPPED=""
  out=$(printf '%s' "$ALL" | jq -r "$filter" 2>/dev/null || true)
  # AN ISSUE SOMEBODY IS ALREADY ON IS NOT WORK TO START. Dropped rather than hidden: the count is
  # printed, because "nothing here" and "three of these are in flight" are different answers.
  # --landed: only Issues whose branch is GONE from the remote, i.e. the work merged and the branch
  # was deleted, or there never was one. An Issue still holding an open branch is somebody else's
  # turn, and showing it here is how a role goes looking for work that does not exist yet.
  [ "$skip" != "--landed" ] || drop head "${VERIFIED:-}" "you have already recorded a verdict on this"
  if [ "$skip" = "--landed" ]; then
    drop head "${OPEN_BRANCH_ISSUES:-}" "still has an open pull request — somebody else's turn"
    # AND AN ISSUE NOBODY HAS EVER BUILT IS NOT LANDED EITHER. Named too: "nothing here" and "four
    # of these have not been built yet" are different answers, and the second is the common one on
    # a new board.
    local notbuilt
    notbuilt=$(printf '%s\n' "$out" | sed -n 's/^  #\([0-9][0-9]*\) .*/\1/p')
    [ -z "${EVER_BUILT:-}" ] || notbuilt=$(printf '%s\n' "$notbuilt" | grep -vxF -f <(printf '%s\n' "$EVER_BUILT") || true)
    drop head "$notbuilt" "not built yet — dev has not opened a pull request for it"
  fi
  # --unbuilt: unclaimed AND never built. An Issue whose work already merged is not dev's to
  # resolve — a dev agent spent a whole round discovering that by hand, which is a round the queue
  # could have saved it.
  # --unruled: drop the ones whose decision has since been answered.
  [ "$skip" != "--unruled" ] || drop head "${RULED:-}" "already ruled on — the decision is made"

  if [ "$skip" = "--unbuilt" ] && [ -n "${EVER_BUILT:-}" ]; then
    # A RULING MAKES A BUILT ISSUE UNBUILT AGAIN. What shipped answered nothing; the answer has
    # arrived, and the work it implies has not been done.
    local built=$EVER_BUILT
    [ -z "${RULED:-}" ] || built=$(printf '%s\n' "$EVER_BUILT" | grep -vxF -f <(printf '%s\n' "$RULED") || true)
    drop head "$built" "a pull request has already been opened for this"
  fi
  [ "$skip" = "--unbuilt" ] && skip=--unclaimed
  if [ "$skip" = "--unclaimed" ] && [ -n "${CLAIMED:-}" ]; then
    local before after
    local out_before=$out
    before=$(printf '%s\n' "$out" | grep -c . || true)
    out=$(printf '%s\n' "$out" | grep -vE "^  #($(printf '%s' "$CLAIMED" | tr '\n' '|' | sed 's/|$//'))  " || true)
    after=$(printf '%s\n' "$out" | grep -c . || true)
    # NAMED, NOT COUNTED. `(1 already have a branch — not shown)` is unfalsifiable from the output,
    # and it hid a real unclaimed Issue: another agent's branch happened to carry the number 4 in
    # its slug — `product/chore/4-archive-...`, about a pull request, nothing to do with Issue #4 —
    # so the claim set swallowed it. A dev agent found it only by listing Issues by hand, and
    # called it what it is: a second agent silently deleting an item from its queue while the
    # tooling returned rc=0 and a reassuring parenthesis.
    #
    # Naming them costs nothing and makes a wrong claim visible the moment it happens.
    [ "$before" -eq "$after" ] || DROPPED="$DROPPED
  (already has an open pull request — somebody is on it. If that is wrong, its branch names the wrong number:)
$(printf '%s\n' "$out_before" | grep -E "^  #($(printf '%s' "$CLAIMED" | tr '\n' '|' | sed 's/|$//'))  " | sed 's/^  /      /' || true)"
  fi
  printf '\n%s\n' "$head"
  if [ -n "$out" ]; then printf '%s\n' "$out"; else printf '  (none)\n'; fi
  [ -z "$DROPPED" ] || printf '%s\n' "$DROPPED"
}

# YOUR OWN PULL REQUESTS, AND WHICH OF THEM NEED YOU. Without this a role has to call three
# separate things to work out what to do next, and the previous build showed what happens then: an
# agent fixed the one red it had been told about and never saw the one blocking the merge.
#
# THE VERDICT IS COMPUTED BY pr.sh, NOT RE-IMPLEMENTED HERE. It reads both the check runs and the
# commit statuses, and duplicating that logic is how the two answers drift apart.
# WHICH PULL REQUESTS ARE MINE IS NOT ALWAYS "THE ONES I AUTHORED". dev owns the branches it wrote;
# product owns the FEATURE pull requests whoever wrote them, because UAT is done on somebody else's
# branch. Filtering product by a `product/*` prefix returned (none) for a round whose entire workload
# was two dev branches — a successful lookup that filtered the answer away, which is worse than a
# failed one because the exit code is 0.
my_prs() {
  local match=$1 heading=${2:-YOUR PULL REQUESTS} prs line num branch st
  # RESOLVED HERE TOO. `issues()` calls resolve_repo inside a command substitution, so REPO is set in
  # a subshell that has already exited — the variable is unset by the time this runs. Found by
  # running it, not by reading it.
  resolve_repo
  prs=$(api --paginate "repos/$REPO/pulls?state=open&per_page=100")
  printf '\n%s:\n' "$heading"
  local any=0
  while IFS=$'\t' read -r num branch title; do
    [ -n "$num" ] || continue
    # THE PULL REQUEST'S TYPE IS IN ITS BRANCH NAME — `<role>/<type>/<issue>-<slug>`, which the
    # naming gate already enforces. Routing on it means a chore reaches qa and a feature reaches
    # product without anything being stored twice. Matching every branch instead put one archive
    # pull request in both queues at once, and two roles racing to merge the same thing is exactly
    # the collision this queue exists to prevent.
    local matched=0 pat
    IFS='|' read -ra _pats <<< "$match"
    for pat in "${_pats[@]}"; do case "$branch" in $pat) matched=1; break ;; esac; done
    [ "$matched" -eq 1 ] || continue
    any=1
    st=$("$(dirname "${BASH_SOURCE[0]}")/pr.sh" state "$num" --brief 2>&1) || true
    printf '  #%-4s %-46s %s\n' "$num" "$(printf '%s' "$title" | cut -c1-46)" "$st"
  done < <(printf '%s' "$prs" | jq -r '.[] | [.number, .head.ref, .title] | @tsv' 2>/dev/null || true)
  [ "$any" -eq 1 ] || printf '  (none)\n'
}

# REVIEWS ARE WORK, AND UNTIL NOW THEY WERE IN NOBODY'S QUEUE.
#
# THIS IS THE DEADLOCK THIS FUNCTION EXISTS TO END, and it was reached: eleven pull requests open,
# eight of them red for want of an independent verdict, every one built by `dev` and `product` — so
# `qa` was the only role the gate would accept. `queue.sh qa` printed two headings and `(none)`
# under both, correctly, because it routes pull requests by branch type and every one of those was
# a `feat/*` belonging to product's merge queue. qa read its queue, learned it had no work, and
# stopped. **It was the only role that could unblock the entire board.**
#
# Review reached a role through exactly one channel: a `NEEDS-REVIEW` event from a live watcher.
# That is a push channel, and a push channel is a thing that can be missed — the watcher dies, the
# session ends, the agent restarts, and the work becomes invisible with no trace that it ever
# existed. **`queue.sh` is the pull channel and the prompts call it "your queue", so anything absent
# from it is work the process cannot recover.** A queue that answers "nothing" while eight pull
# requests wait on you is not an empty queue; it is a wrong answer with a zero exit code.
#
# INDEPENDENCE IS DERIVED EXACTLY AS THE GATE DERIVES IT — the `Agent:` trailers of the pull
# request's own commit list, over the API. Not a merge-base range in this clone: those are
# different sets of commits, and a reviewer cleared by the second one had to withdraw a verdict it
# had already posted. If this offers you a pull request, the gate will accept your verdict on it.
# HAS THIS ROLE ALREADY RULED ON THIS EXACT HEAD? Reads the attestation `check-review.sh` reads —
# `Reviewed-by: <role>` and `Reviewed-sha: <head>` in a comment on the pull request — so there is one
# record of "a verdict happened" and both the gate and this queue read it. Nothing is stored twice.
#
# A LOOKUP FAILURE IS NOT "NOT YET REVIEWED". `api` exits non-zero rather than returning empty, for
# the same reason every other query here does: the alternative is a queue that hands out work a role
# has already done because GitHub was briefly unreachable.
#
# `(^|\n)` AND `(\r?\n|$)`, NOT `^`/`$`. jq's `test` is Oniguruma and its `"m"` flag means "dot
# matches newline" — it is NOT Perl's `/m`, and `$` stays anchored to the end of the whole body. The
# first version used `^…$` with `"m"` and matched NOTHING, so every verdict looked absent and the
# fix silently did not apply. It went red in `internal/machinery` on the first run, which is the
# only reason it is not shipped that way.
#
# The tail anchor also stops `qa` matching a longer name and a short sha matching a longer one. It
# errs toward "not reviewed", i.e. toward offering the work again — the failure this replaces, and
# the safe direction to be wrong in.
# --- the review history of one pull request, read once --------------------------------------
#
# ONE FETCH PER PULL REQUEST PER RUN. Every question below — who owns the review, how many rounds it
# has been through, whether YOU have ruled on THIS head — is answered from the same comments. Asking
# separately cost one paginated call per question per pull request per role, and the budget is
# shared with three roles and four watches: measured 114 rate-limit refusals against `qa` in one
# day, on a board of six pull requests. A queue that exhausts the budget reports `LOOKUP FAILED` for
# polls its own polling made impossible.
CACHE=$(mktemp -d 2>/dev/null || echo "")
cleanup_cache() { [ -n "${CACHE:-}" ] && rm -rf "$CACHE"; }
trap cleanup_cache EXIT

# CALLED OUTSIDE A COMMAND SUBSTITUTION, ON PURPOSE. `api` exits the shell on a failed lookup, and
# that exit is swallowed by a subshell — the documented trap two functions down. Fetching here and
# reading the file there keeps the outage fatal instead of turning it into "nobody has reviewed it".
ensure_pr_comments() { # ensure_pr_comments <pr-number>
  local num=$1
  [ -n "$CACHE" ] || { echo "::error::no writable temporary directory; refusing to guess at review state." >&2; exit 1; }
  [ -f "$CACHE/pr-$num.json" ] && return 0
  api --paginate "repos/$REPO/issues/$num/comments?per_page=100" > "$CACHE/pr-$num.json.part"
  mv "$CACHE/pr-$num.json.part" "$CACHE/pr-$num.json"
}

# THE SAME RULES THE GATE USES, AND FOR THE SAME REASONS — see check-review.sh, which argues both at
# length. Fenced text is stripped before anything is parsed, because a comment QUOTING the verdict
# template read as a verdict and did so under whoever the quote happened to name. And the reviewer is
# the `[role]` marker on the first line, not the name inside the text: every role here posts through
# one GitHub account, so `.user.login` would make all of them one reviewer and switch independence
# off entirely.
#
# Emits one line per verdict, oldest first:  <role>\t<sha>\t<approve|changes>
pr_verdicts() { # pr_verdicts <pr-number>   — ensure_pr_comments must have run
  local num=$1
  jq -r '
    def strip_fences:
      split("\n")
      | reduce .[] as $l ({inb:false, out:[]};
          if ($l | test("^[[:space:]]{0,3}(```|~~~)")) then .inb = (.inb | not)
          elif .inb then .
          else .out += [$l] end)
      | .out | join("\n");
    .[]
    | ((.body // "") | gsub("\r"; "")) as $raw
    | ($raw | strip_fences | split("\n")) as $lines
    | select($lines | any(test("^Reviewed-by:")))
    | select($lines | any(test("^Verdict:")))
    | (($raw | split("\n")[0] | capture("^\\[(?<r>[A-Za-z][A-Za-z0-9_-]*)\\][[:space:]]*$") | .r) // "") as $role
    | ($lines | map(select(test("^Reviewed-sha:"))) | .[0] // "" | sub("^Reviewed-sha:[[:space:]]*"; "") | sub("[[:space:]]*$"; "")) as $sha
    | ($lines | map(select(test("^Verdict:")))     | .[0] // "" | sub("^Verdict:[[:space:]]*"; "")     | sub("[[:space:]]*$"; "")) as $v
    | select($role != "")
    | [$role, $sha, (if ($v | test("^changes")) then "changes" else "approve" end)]
    | @tsv
  ' < "$CACHE/pr-$num.json" 2>/dev/null || true
}

# HAS THIS ROLE ALREADY RULED ON THIS EXACT HEAD?
reviewed_by() { # reviewed_by <pr-number> <role> <head-sha>
  local num=$1 role=$2 sha=$3
  # THE SHA IS PART OF THE ANSWER, which is what makes this release itself: a push moves the head,
  # every prior verdict stops matching, and the work reappears. It errs toward "not reviewed" —
  # toward offering the work again, which is the safe direction to be wrong in.
  pr_verdicts "$num" | awk -F'\t' -v r="$role" -v s="$sha" '$1==r && $2==s {found=1} END{exit !found}'
}

# WHOSE REVIEW IS THIS? THE ROLE THAT TOOK IT FIRST, AND IT KEEPS IT.
#
# THIS IS THE PING-PONG, AND IT IS THE MOST EXPENSIVE DEFECT ON THE BOARD. Measured on #53:
# **eleven verdicts over eighteen hours — seven `changes-requested` and four `approve`, alternating
# between qa and product — 32 comments, and it was still open and unmerged with every check green.**
#
# No function was wrong. The mechanism was: a `changes-requested` costs a push, a push moves the
# head, and a moved head makes every prior verdict stale and re-opens the pull request to EVERY
# independent role. So the next round was reviewed by a DIFFERENT agent, against a different
# standard, which raised findings the first one had already considered and passed. Each side was
# individually reasonable and the pair of them did not converge, because nothing in the process
# made the second reviewer answerable to the first one's judgement.
#
# THE FIX IS THAT A REVIEW HAS AN OWNER. Whoever posts the first verdict owns every later round of
# that pull request. They see their own findings, they know what they already cleared, and a
# re-review is scoped to what changed — which is a question that terminates.
#
# DERIVED, NOT STORED. It is the first verdict in the comment history; there is no label to set and
# nothing to expire. And it CANNOT STRAND ANYTHING, which is the failure this must not reintroduce
# (#32: a single eligible reviewer died and the board stopped):
#   - it holds only while the owner is still independent of the head. A pull request that grows a
#     commit authored by its reviewer releases the ownership automatically, because the same
#     independence test that offers the work withdraws it.
#   - it is visible to everybody. Other roles see the pull request under `REVIEW OWNED BY <role>`
#     rather than not at all, so a stalled review is something a role can see and say so about,
#     not something that has silently vanished from every queue.
#   - it lapses entirely at the escalation limit below, at which point the pull request stops being
#     a review problem.
review_owner() { # review_owner <pr-number>
  pr_verdicts "$1" | awk -F'\t' 'NR==1 {print $1; exit}'
}

# HOW MANY TIMES HAS THIS BEEN SENT BACK? A number, because three is not a review and it is not a
# disagreement either — it is a question the process cannot answer, and the process must say so
# rather than spend another round finding out again.
changes_rounds() { # changes_rounds <pr-number>
  pr_verdicts "$1" | awk -F'\t' '$3=="changes" {n++} END{print n+0}'
}

# THE POINT AT WHICH REVIEWING AGAIN IS NOT THE ANSWER. Configurable because a project may know its
# own tolerance; three because #53 was still not converging at seven.
REVIEW_MAX_ROUNDS=${ADF_REVIEW_MAX_ROUNDS:-3}

# REVIEW IS NO LONGER SOMETHING ANOTHER ROLE DISCOVERS. IT HAPPENS BEFORE THE WORK IS HANDED OVER.
#
# WHAT THIS FUNCTION USED TO DO, AND WHY IT IS GONE. It offered every open pull request to every
# role independent of its commits, so that a review nobody had been assigned would still reach
# somebody. It worked — it ended the deadlock where eight red pull requests sat in nobody's queue —
# and it brought its own failure, which was worse:
#
#   Measured on #53: eleven verdicts in eighteen hours, seven `changes-requested` and four
#   `approve`, alternating between qa and product, 32 comments, still open and unmerged with every
#   check green. A `changes-requested` costs a push; a push moves the head; a moved head makes
#   every verdict stale and re-opens the pull request to EVERY independent role — so the next round
#   was judged by a different agent against a different standard, raising findings the previous one
#   had considered and passed. Nobody was wrong and it did not converge.
#
# THE REVIEW IS NOW SYNCHRONOUS AND LOCAL. The author dispatches an independent reviewer as a
# sub-agent the moment the branch is ready, in the author's own session: it reviews in a worktree,
# posts its verdict, and hands its findings back in the same turn. The author fixes and goes back to
# THE SAME reviewer, which still has its own findings in context. One reviewer, one standard, and a
# loop that terminates because both halves are in one conversation.
#
# WHAT THAT BUYS, BEYOND CONVERGENCE. The old design's every question — has anyone reviewed this,
# has this role reviewed this head, is this role independent of it — was a poll against a shared API
# budget, by three roles and four watches. Measured in one day: 246 rate-limit refusals, and the
# watches spending the budget the roles needed to work. A review that happens in the session that
# produced the work asks nobody anything.
#
# AND IT COSTS NOTHING IN INDEPENDENCE, WHICH IS THE PART THAT LOOKS LIKE A LOSS AND IS NOT. Every
# role here posts through ONE GitHub account: `check-review.sh` says so at length, and independence
# has always been the `[role]` marker, a convention rather than an authenticated fact. A sub-agent
# with its own context that authored none of the commits is exactly as independent as a separate
# session was, and it is the same reviewer prompt doing the same work.
#
# THE GATE IS UNCHANGED AND IS STILL THE BACKSTOP. `check-review.sh` still refuses any head with no
# independent verdict on it, so a pull request whose review was skipped cannot merge. This function
# is now the PULL channel for the one case that leaves: the author opened a pull request and never
# ran the review. That is the author's own work, in the author's own queue, and in nobody else's.
unreviewed_own_prs() {
  local role=$1 num sha branch title prs any=0 owner rounds
  resolve_repo
  prs=$(api --paginate "repos/$REPO/pulls?state=open&per_page=100")
  printf '\nYOUR PULL REQUESTS WITH NO VERDICT ON THE CURRENT HEAD — dispatch a reviewer before anything else:\n'
  while IFS=$'\t' read -r num sha branch title; do
    [ -n "$num" ] || continue

    # WHOSE PULL REQUEST THIS IS COMES FROM ITS BRANCH NAME — `<role>/<type>/<issue>-<slug>`, which
    # the naming gate already enforces and which `my_prs` already routes merges on. It costs nothing.
    #
    # THE ALTERNATIVE WAS MEASURED AND IT WAS WORSE. Deriving it from the `Agent:` trailers means one
    # API call per open pull request per role per round, to answer a question the branch name already
    # answers: on a six-pull-request board that made dev's queue MORE expensive than the design it
    # replaced, in a change whose point was to spend less.
    #
    # AND IT IS NOT THE INDEPENDENCE TEST. Nothing here decides who may certify anything — that is
    # `check-review.sh`, which re-derives authorship from the trailers at verdict time and refuses an
    # author's own verdict. This only decides whose queue a pull request appears in. The two were the
    # same question when any independent role might be sent to review; they are not any more.
    case "$branch" in
      "$role"/*) : ;;
      *) continue ;;
    esac

    # FETCHED OUTSIDE EVERY SUBSTITUTION — for the pull requests that are yours, and no others — so a
    # failed lookup exits rather than becoming "no verdict exists", which reads as "go and review it"
    # on work already reviewed.
    ensure_pr_comments "$num"

    # THREE ROUNDS AND IT IS NOT A REVIEW PROBLEM ANY MORE. Below the limit this says nothing: a
    # second round is an ordinary review doing its job. At the limit it stops asking for another one.
    rounds=$(changes_rounds "$num")
    if [ "$rounds" -ge "$REVIEW_MAX_ROUNDS" ]; then
      any=1
      printf '  #%-4s %-46s  ESCALATED — %s rounds of changes; do NOT push again\n' \
        "$num" "$(printf '%s' "$title" | cut -c1-46)" "$rounds"
      printf '        %s\n' "Say so on the pull request and hand it to product, which is the only role that may put it to the owner."
      continue
    fi

    # A VERDICT ON THIS EXACT HEAD IS THE ANSWER. The sha is part of it, so a push releases it and
    # the work reappears here — which is what makes the fix loop visible without storing anything.
    owner=$(review_owner "$num")
    if [ -n "$owner" ] && reviewed_by "$num" "$owner" "$sha"; then continue; fi
    any=1
    if [ -n "$owner" ]; then
      # GO BACK TO THE ONE THAT ALREADY LOOKED. Naming it is the whole of the anti-ping-pong rule at
      # this layer: a fresh reviewer re-opens findings the first one settled.
      printf '  #%-4s %-46s  re-review by %s (round %s)\n' \
        "$num" "$(printf '%s' "$title" | cut -c1-46)" "$owner" "$((rounds + 1))"
    else
      printf '  #%-4s %-46s  NO REVIEW HAS HAPPENED — dispatch one now\n' \
        "$num" "$(printf '%s' "$title" | cut -c1-46)"
    fi
  done < <(printf '%s' "$prs" | jq -r '.[] | [.number, .head.sha, .head.ref, .title] | @tsv' 2>/dev/null || true)
  [ "$any" -eq 1 ] || printf '  (none)\n'
}

# WHAT THE REVIEW LOOP COULD NOT SETTLE. Product's section, and product's alone, because product is
# the only role that may put anything to the owner.
#
# THIS EXISTS BECAUSE THE ALTERNATIVE WAS MEASURED. #53 went seven rounds and stopped being a review
# long before that; there was nowhere for it to go, so it went round again. A disagreement two
# competent agents cannot settle is a question about what the project wants, and that is a decision,
# not a defect.
escalated_reviews() {
  local num sha title prs any=0 rounds
  resolve_repo
  prs=$(api --paginate "repos/$REPO/pulls?state=open&per_page=100")
  printf '\nREVIEWS THAT DID NOT CONVERGE — yours to put to the owner, and nobody else may:\n'
  while IFS=$'\t' read -r num sha title; do
    [ -n "$num" ] || continue
    ensure_pr_comments "$num"
    rounds=$(changes_rounds "$num")
    [ "$rounds" -ge "$REVIEW_MAX_ROUNDS" ] || continue
    any=1
    printf '  #%-4s %-46s  %s rounds of changes-requested\n' \
      "$num" "$(printf '%s' "$title" | cut -c1-46)" "$rounds"
  done < <(printf '%s' "$prs" | jq -r '.[] | [.number, .head.sha, .title] | @tsv' 2>/dev/null || true)
  [ "$any" -eq 1 ] || printf '  (none)\n'
}

# WHAT IS ALREADY CLAIMED, DERIVED FROM THE BRANCH NAMES. `<role>/<type>/<issue>-<slug>` carries the
# Issue number, so a branch existing IS the claim — there is no label to set, no comment to post and
# nothing to expire. State is stored once, which is the whole reason the naming convention is a gate.
#
# Without this two agents of the same role both take the same Issue: it stayed in "to resolve" while
# its own pull request was open two lines below, which is what running it showed.
claimed_issues() {
  resolve_repo
  # AN OPEN PULL REQUEST IS THE CLAIM, NOT A BRANCH THAT EXISTS. Branches outlive their merges —
  # GitHub keeps them unless somebody deletes them — so a branch-based claim never expires, and an
  # Issue whose work had shipped stayed marked as "somebody is on it" forever. An open pull request
  # ends exactly when the work does, which is the property a claim needs.
  api --paginate "repos/$REPO/pulls?state=open&per_page=100" \
    | jq -r '.[].head.ref' 2>/dev/null \
    | sed -n 's#^[a-z]*/[a-z]*/\([0-9][0-9]*\)-.*#\1#p' | sort -u
}

role_queue() {
  local role=$1
  ALL=$(issues | jq -s 'add // []')
  # AN ISSUE YOU HAVE ALREADY VERIFIED IS NOT WORK WAITING FOR YOU. A product agent UAT'd two
  # Issues, found their criteria unreachable, deliberately left them open and recorded why — and the
  # queue went on listing them under "UAT and CLOSE", telling the next agent to do work already done.
  # A queue that repeats finished work is a queue people stop reading.
  #
  # The signal is the role's own marked comment, which is where the verdict already lives.
  CLAIMED=$(claimed_issues)
  resolve_repo
  # Issues that still have an open branch: not landed. And issues that have EVER had a pull request,
  # open or merged: the ones whose work exists at all.
  OPEN_BRANCH_ISSUES=$CLAIMED
  # Issues already carrying this role's own verdict comment.
  # AN ANSWERED DECISION IS NOT A WAITING ONE, AND THE ANSWER ARRIVES AS A COMMENT. The `##
  # Blocked on a decision` section stays in the body forever — it is the record of what was asked —
  # so a ruling posted underneath left the Issue sitting in "waiting on a decision" with the
  # decision made. And the ruling means the build is now INCOMPLETE against it, so the Issue goes
  # back to dev even though it has been built once.
  # ONE FETCH, THREE QUESTIONS. This endpoint is every comment in the repository, paginated, and it
  # was being pulled THREE times in one run of one role's queue — twice here and once more for the
  # release count. Three roles and four watches share a 5,000/hour budget; measured in one day, 246
  # rate-limit refusals, with the queue reporting LOOKUP FAILED for polls its own polling had made
  # impossible. The three questions differ only in the jq that reads the answer.
  #
  # WRITTEN TO A FILE OUTSIDE A SUBSTITUTION, for the reason documented on `ensure_pr_comments`: a
  # command substitution is a subshell and swallows `api`'s exit, which would turn an outage into
  # "nobody has ruled on anything" — an answer, and the wrong one.
  ALL_COMMENTS="$CACHE/all-comments.json"
  [ -f "$ALL_COMMENTS" ] || api --paginate "repos/$REPO/issues/comments?per_page=100" > "$ALL_COMMENTS"
  RULED=$(jq -r '.[] | select(.body | startswith("**[owner-ruling]") or startswith("[owner-ruling]")) | .issue_url' < "$ALL_COMMENTS" 2>/dev/null \
    | sed -n 's#.*/issues/##p' | sort -u)
  VERIFIED=$(jq -r --arg r "[$role]" '.[] | select(.body | startswith($r)) | .issue_url' < "$ALL_COMMENTS" 2>/dev/null \
    | sed -n 's#.*/issues/##p' | sort -u)
  # MERGED, NOT MERELY OPENED. `state=all` counted a pull request that was CLOSED without merging,
  # so an abandoned attempt removed its Issue from dev's queue permanently — deleting the branch did
  # not help, because the pull request record keeps the ref. An Issue whose only pull request was
  # abandoned has not been built.
  EVER_BUILT=$(api --paginate "repos/$REPO/pulls?state=all&per_page=100" \
    | jq -r '.[] | select(.merged_at != null) | .head.ref' 2>/dev/null \
    | sed -n 's#^[a-z]*/[a-z]*/\([0-9][0-9]*\)-.*#\1#p' | sort -u)

  case "$role" in
    dev)
      emit "ISSUES TO RESOLVE — open one branch and one PR per Issue:" \
        '.[] | select(.pull_request==null)
             | select([.labels[].name] | any(startswith("type:")))
             | select([.labels[].name] | index("blocked") | not)
             | "  #\(.number)  \(.title)"' --unbuilt
      my_prs "dev/*"
      unreviewed_own_prs dev ;;
    qa)
      # NOT EVERY BUG IS YOURS YET. An Issue with no branch has nothing to verify — it is dev's to
      # build first. Listing it under "to verify" is the same defect as the product arm one layer
      # down: a queue that cannot tell "yours to act on now" from "yours eventually" sends a role
      # looking for work that does not exist.
      emit "ISSUES WHOSE WORK HAS LANDED — verify on main and CLOSE:" \
        '.[] | select(.pull_request==null)
             | select([.labels[].name] | index("type:bug") or index("type:chore"))
             | "  #\(.number)  \(.title)"' --landed 
      my_prs "*/fix/*|*/bug/*|*/chore/*|*/docs/*|*/test/*|*/ci/*|*/build/*|*/refactor/*|*/perf/*" "PULL REQUESTS TO VERIFY, MERGE AND CLOSE — whoever wrote them"
      unreviewed_own_prs qa ;;
    product)
      emit "FEATURES WHOSE WORK HAS LANDED — UAT on main and CLOSE (already verified ones are dropped):" \
        '.[] | select(.pull_request==null)
             | select([.labels[].name] | index("type:feature"))
             | "  #\(.number)  \(.title)"' --landed 
      my_prs "*/feat/*|*/spec/*" "PULL REQUESTS TO UAT, MERGE AND CLOSE — whoever wrote them"
      unreviewed_own_prs product
      escalated_reviews ;;
    owner)
      # THE OWNER HAD NO QUEUE, AND EVERY ROLE HAD ONE.
      #
      # Measured: a product agent formed a release verdict — "do not ship bbee48f, four blockers" —
      # and it reached the owner because they happened to be reading that window at that moment. The
      # sha appeared in NO file, NO Issue and NO label; it existed in one terminal message. Had they
      # been away, nothing would have told them, and nothing would have stopped the release either.
      # The owner's own words: **"product 都没问我，我怎么知道我要决定?"**
      #
      # This is the orphan defect one level up. Every filter was right, every role was doing its job,
      # and the one decision nobody else may take was addressed to a human through no channel at all.
      #
      # DERIVED, LIKE EVERY OTHER QUEUE. No coordinator maintains this: a blocker is an open Issue
      # labelled `blocks:release`, and a question is an Issue whose body carries `## Blocked on a
      # decision` with no ruling yet. Both are states the roles already produce.
      emit "DECISIONS ONLY YOU CAN MAKE — the work proceeds around them, refusing where they bite:" \
        '.[] | select(.pull_request==null)
             | select(.body // "" | test("## Blocked on a decision"))
             | "  #\(.number)  \(.title)"' --unruled

      emit "HOLDING THE RELEASE — these are why nothing can ship yet:" \
        '.[] | select(.pull_request==null)
             | select([.labels[].name] | index("blocks:release"))
             | "  #\(.number)  \(.title)"'

      # AND WHETHER ANYONE HAS SAID ANYTHING ABOUT SHIPPING AT ALL. "No blockers" and "nobody has
      # looked" are different answers and this is the page where confusing them is most expensive:
      # one means ship, the other means you have been told nothing.
      local nb rel
      nb=$(printf '%s' "$ALL" | jq '[.[]|select(.pull_request==null)|select([.labels[].name]|index("blocks:release"))]|length')
      rel=$(jq -r '[.[] | select(.body | test("^\\[product\\][\\s\\S]*RELEASE"))] | length' < "$ALL_COMMENTS" 2>/dev/null || echo 0)
      printf '\nRELEASE\n'
      if [ "${nb:-0}" -gt 0 ]; then
        printf '  BLOCKED — %s Issue(s) labelled blocks:release are open, listed above.\n' "$nb"
      elif [ "${rel:-0}" -eq 0 ]; then
        printf '  UNDETERMINED — nothing is labelled blocks:release AND product has recorded no release\n'
        printf '  verdict. That is NOT "ready to ship": it is nobody having said. Ask product for a verdict\n'
        printf '  before reading this as a green light.\n'
      else
        printf '  No open blocker. product has recorded %s release verdict(s) — read the most recent\n' "$rel"
        printf '  before calling one; a verdict is about a specific sha and yours may be older than main.\n'
      fi
      ;;
    ops)
      emit "OPEN PULL REQUESTS — CI and gate health:" \
        '.[] | select(.pull_request!=null) | "  #\(.number)  \(.title)"' ;;
    pm)
      # DECISIONS THE OWNER OWES — NOT WORK NOBODY CAN DO. The first version said "nobody can build
      # these", which contradicted dev's own instruction: an Issue carrying an open decision IS
      # dev's to build, as far as its criteria settle, with the undecided path refusing loudly. The
      # same Issue then appeared in dev's "resolve these" and pm's "nobody can build these" at once.
      #
      # What is true is narrower and still worth a section: these carry a question only the owner
      # can answer, and the pm is the only channel to them. Some will also be in dev's queue, and
      # that is not a contradiction — part of the work is buildable and part of it is not.
      emit "DECISIONS THE OWNER OWES — the work proceeds around them, refusing where they bite:" \
        '.[] | select(.pull_request==null)
             | select(.body // "" | test("## Blocked on a decision"))
             | "  #\(.number)  \(.title)"' --unruled
      emit "UNTYPED — cannot be routed until they carry a type: label:" \
        '.[] | select(.pull_request==null)
             | select([.labels[].name] | any(startswith("type:")) | not)
             | "  #\(.number)  \(.title)"'
      emit "UNCLASSIFIED — no area: label, so the R7 ratio cannot see them:" \
        '.[] | select(.pull_request==null)
             | select([.labels[].name] | any(startswith("area:")) | not)
             | "  #\(.number)  \(.title)"'
      # R7's ratio and R8's net numbers, printed every round whether or not anyone asks.
      local p m
      p=$(printf '%s' "$ALL" | jq '[.[]|select(.pull_request==null)|select([.labels[].name]|index("area:product"))]|length')
      m=$(printf '%s' "$ALL" | jq '[.[]|select(.pull_request==null)|select([.labels[].name]|index("area:machinery"))]|length')
      printf '\nRATIO (PRD R7) — product %s : machinery %s\n' "$p" "$m"
      [ "$m" -le "$p" ] || printf '  OVER THE CAP. Dispatch no further machinery work until this is 1:1 or better.\n'
      ;;
    *)
      echo "::error::'$role' is not a role. One of: dev qa product ops pm owner" >&2; return 1 ;;
  esac
}

self_test() {
  local rc=0
  # A role that is not in the list must be REFUSED, not silently given an empty queue — an empty
  # queue is indistinguishable from "you have no work" and that is the defect this project is about.
  ( REPO=x/y role_queue not-a-role ) >/dev/null 2>&1 \
    && { echo "SELF-TEST FAIL: an unknown role was accepted" >&2; rc=1; }

  # Every role in the documented set must be dispatchable. A role prompt that exists with no queue
  # arm is a role that can never find its work.
  local r
  for r in dev qa product ops pm owner; do
    grep -q "^    $r)" "${BASH_SOURCE[0]}" \
      || { echo "SELF-TEST FAIL: role '$r' has no queue arm" >&2; rc=1; }
  done

  # A failed lookup must exit non-zero rather than print an empty queue.
  grep -q 'LOOKUP FAILURE and NOT a statement' "${BASH_SOURCE[0]}" \
    || { echo "SELF-TEST FAIL: a failed lookup is not distinguished from an empty queue" >&2; rc=1; }

  # AN UNREVIEWED PULL REQUEST MUST APPEAR IN ITS AUTHOR'S QUEUE, AND IN NO OTHER ROLE'S.
  #
  # THIS ARM REPLACES THE DEADLOCK ARM AND INHERITS ITS REASON. The defect it guarded — eight red
  # pull requests in nobody's queue, every role's exit code 0 — was not "the code is wrong": every
  # function did what it said, and no arm of this script asked the question at all. That is still the
  # failure mode. What changed is who the question is for: the author gets its own work reviewed
  # before handing it on, so an unreviewed pull request is the author's outstanding work and nobody
  # else's.
  #
  # BOTH DIRECTIONS, because either alone is passed by a wrong fix. Missing from the author's queue
  # is the old outage wearing a new name; present in everyone else's is the ping-pong that cost #53
  # eleven verdicts.
  local tmp out
  tmp=$(mktemp -d)
  cat > "$tmp/gh" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *"pulls/9/commits"*)  printf 'c9\tAgent: dev\n' ;;
  *"/commits/c9"*)      echo 'internal/a.go' ;;
  *"pulls?state=open"*) echo '[{"number":9,"head":{"ref":"dev/feat/9-x","sha":"cafe"},"title":"feat(x): y"}]' ;;
  *"issues/9/comments"*) printf '%s' "${STUB_COMMENTS:-[]}" ;;
  *"/status"*)          echo '{"statuses":[]}' ;;
  *)                    echo '[]' ;;
esac
STUB
  chmod +x "$tmp/gh"
  # THE STUB RETURNS WHAT `gh --jq` WOULD RETURN, NOT RAW JSON. It does not implement `--jq`, so a
  # fixture that hands back the unextracted object silently feeds every caller a blob that parses as
  # nothing — the arm then fails for a reason that has nothing to do with what it is testing.
  out=$( PATH="$tmp:$PATH" REPO=x/y bash "${BASH_SOURCE[0]}" dev 2>&1 || true )
  case "$out" in
    *"NO REVIEW HAS HAPPENED"*) : ;;
    *) echo "SELF-TEST FAIL: dev built #9, no verdict exists on its head, and dev's queue did not say so — an unreviewed pull request is now in NOBODY's queue, which is the outage this arm inherited (got: $out)" >&2; rc=1 ;;
  esac
  out=$( PATH="$tmp:$PATH" REPO=x/y bash "${BASH_SOURCE[0]}" qa 2>&1 || true )
  case "$out" in
    *"NO REVIEW HAS HAPPENED"*) echo "SELF-TEST FAIL: a pull request dev built and must get reviewed was listed as qa's work too. That is the ping-pong: two roles reviewing one branch against two standards, which on #53 ran to eleven verdicts without converging (got: $out)" >&2; rc=1 ;;
    *) : ;;
  esac

  # A VERDICT ON THIS HEAD BY THE ROLE THAT OWNS THE REVIEW SETTLES IT.
  local att='[{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: cafe\nVerdict: approve"}]'
  out=$( PATH="$tmp:$PATH" REPO=x/y STUB_COMMENTS="$att" bash "${BASH_SOURCE[0]}" dev 2>&1 || true )
  case "$out" in
    *"NO REVIEW HAS HAPPENED"*|*"re-review by"*) echo "SELF-TEST FAIL: #9 carries a verdict on its current head and the queue still asked for a review — that is a second review of work already reviewed (got: $out)" >&2; rc=1 ;;
    *) : ;;
  esac

  # AND IT RELEASES ITSELF ON A PUSH. The sha is part of the verdict, so one naming another head
  # holds nothing — which is why a review cannot strand the head it does not describe.
  out=$( PATH="$tmp:$PATH" REPO=x/y STUB_COMMENTS='[{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: 0000\nVerdict: approve"}]' bash "${BASH_SOURCE[0]}" dev 2>&1 || true )
  case "$out" in
    *"re-review by qa"*) : ;;
    *) echo "SELF-TEST FAIL: a verdict naming a DIFFERENT head suppressed this one — a stale review strands the head it does not describe (got: $out)" >&2; rc=1 ;;
  esac

  # THE RE-REVIEW GOES BACK TO THE ROLE THAT ALREADY LOOKED, AND THE QUEUE NAMES IT. A fresh
  # reviewer re-opens findings the first one settled; naming the owner is the whole of the rule.
  case "$out" in
    *"re-review by qa"*) : ;;
    *) echo "SELF-TEST FAIL: the queue did not name the reviewer that already looked at #9, so the next round could go to a different one (got: $out)" >&2; rc=1 ;;
  esac

  # A QUOTED VERDICT IS NOT A VERDICT. The gate learned this the expensive way — a comment quoting
  # the template read as a real verdict, attributed to whoever the quote happened to name — and this
  # script now parses the same comments, so it must honour the same fences or the two disagree.
  out=$( PATH="$tmp:$PATH" REPO=x/y STUB_COMMENTS='[{"body":"[qa]\nfor reference:\n```\nReviewed-by: qa\nReviewed-sha: cafe\nVerdict: approve\n```"}]' bash "${BASH_SOURCE[0]}" dev 2>&1 || true )
  case "$out" in
    *"NO REVIEW HAS HAPPENED"*) : ;;
    *) echo "SELF-TEST FAIL: a verdict inside a code fence was counted as a review of head 'cafe', so quoting the template certifies a pull request (got: $out)" >&2; rc=1 ;;
  esac

  # THREE ROUNDS OF CHANGES AND IT STOPS BEING A REVIEW PROBLEM. Below the limit the queue asks for
  # another round; at the limit it refuses to and says who may take it. Both directions, because a
  # cap that never fires and a cap that always fires are both silent about the thing they measure.
  local thrash='[{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a1\nVerdict: changes-requested"},{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a2\nVerdict: changes-requested"},{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a3\nVerdict: changes-requested"}]'
  out=$( PATH="$tmp:$PATH" REPO=x/y STUB_COMMENTS="$thrash" bash "${BASH_SOURCE[0]}" dev 2>&1 || true )
  case "$out" in
    *ESCALATED*) : ;;
    *) echo "SELF-TEST FAIL: #9 has been sent back three times and the queue asked for a fourth round. #53 ran to seven and never converged; a disagreement that survives three reviews is a decision, and asking the same question again cannot produce one (got: $out)" >&2; rc=1 ;;
  esac
  case "$out" in
    *"do NOT push again"*) : ;;
    *) echo "SELF-TEST FAIL: the escalation named no next step, so the author's only visible option is still to push again (got: $out)" >&2; rc=1 ;;
  esac
  local twice='[{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a1\nVerdict: changes-requested"},{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a2\nVerdict: changes-requested"}]'
  out=$( PATH="$tmp:$PATH" REPO=x/y STUB_COMMENTS="$twice" bash "${BASH_SOURCE[0]}" dev 2>&1 || true )
  case "$out" in
    *ESCALATED*) echo "SELF-TEST FAIL: two rounds of review escalated to the owner. A second round is an ordinary review working; escalating it spends the owner on what the process is for (got: $out)" >&2; rc=1 ;;
    *) : ;;
  esac

  # AND THE ESCALATION REACHES PRODUCT, WHICH IS THE ONLY ROLE THAT MAY PUT IT TO THE OWNER. An
  # escalation nobody is holding is the orphan defect one level up: measured on a release verdict
  # that existed in one terminal window and reached the owner by luck.
  out=$( PATH="$tmp:$PATH" REPO=x/y STUB_COMMENTS="$thrash" bash "${BASH_SOURCE[0]}" product 2>&1 || true )
  case "$out" in
    *"DID NOT CONVERGE"*) : ;;
    *) echo "SELF-TEST FAIL: a review that ran out of rounds was in nobody's queue — dev was told to stop and product was never told to pick it up, so it stops there (got: $out)" >&2; rc=1 ;;
  esac

  # AND A FAILED COMMENT LOOKUP MUST OFFER NOTHING AND SAY SO.
  #
  # THIS ARM INHERITS ISSUE #79 AND ITS SUBJECT HAS MOVED. That defect was `2>/dev/null || echo ""`
  # around the AUTHOR lookup: it turned a failed call into an empty author set, and a pull request
  # was offered for review to the role that had written every commit in it, `(built by )`. This
  # script no longer asks who authored anything — the branch name answers it, and the independence
  # test that does still need the trailers lives in `check-review.sh`, which has its own arm for
  # exactly this and re-derives from git besides.
  #
  # WHAT REMAINS IS THE SAME SHAPE ON A DIFFERENT CALL. The review history of a pull request is read
  # over the API, and if THAT collapses to "no verdicts" the queue says `NO REVIEW HAS HAPPENED`
  # about work that has been reviewed — sending a role to review it again — or silently omits it,
  # which is the same wrong answer with a zero exit code.
  #
  # Narrowed to ONE endpoint on purpose: a stub that fails everything cannot test this, because the
  # queue dies on the first failed call and the arm goes green whether or not this path handles
  # anything.
  local tmp3
  tmp3=$(mktemp -d)
  cat > "$tmp3/gh" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *"issues/9/comments"*)
    echo 'You have exceeded a secondary rate limit' >&2; exit 1 ;;
  *"pulls?state=open"*) echo '[{"number":9,"head":{"ref":"dev/feat/9-x","sha":"cafe"},"title":"feat(x): y"}]' ;;
  *"/status"*)          echo '{"statuses":[]}' ;;
  *)                    echo '[]' ;;
esac
STUB
  chmod +x "$tmp3/gh"
  local arc=0
  out=$( PATH="$tmp3:$PATH" REPO=x/y bash "${BASH_SOURCE[0]}" dev 2>&1 ) || arc=$?
  [ "$arc" -ne 0 ] || {
    echo "SELF-TEST FAIL: the review history could not be read and the queue exited 0 — 'could not determine whether this was reviewed' and 'determined that it was not' have collapsed, and an agent reads a zero exit as an answer" >&2; rc=1; }
  case "$out" in
    *"NO REVIEW HAS HAPPENED"*) echo "SELF-TEST FAIL: a failed lookup was rendered as 'no review has happened', which sends a role to review work that may already have been reviewed (got: $out)" >&2; rc=1 ;;
  esac
  case "$out" in
    *"::error::"*) : ;;
    *) echo "SELF-TEST FAIL: the lookup failed and nothing was printed — a non-zero exit with no reason reads as a bug in the queue rather than an outage (got: $out)" >&2; rc=1 ;;
  esac
  rm -rf "$tmp3"
  rm -rf "$tmp"

  # THE OWNER'S QUEUE MUST NOT REPORT SILENCE AS A GREEN LIGHT. **This is the whole reason the arm
  # exists.** A board with no `blocks:release` label and no recorded verdict means nobody has looked;
  # reading that as "ready to ship" is the release-shaped version of every other defect here, and it
  # is the one that cannot be undone afterwards.
  local otmp oout
  otmp=$(mktemp -d)
  cat > "$otmp/gh" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *"issues?state=open"*) echo '[{"number":5,"title":"a thing","labels":[{"name":"type:bug"}],"body":""}]' ;;
  *"issues/comments"*)   echo '[]' ;;
  *)                     echo '[]' ;;
esac
STUB
  chmod +x "$otmp/gh"
  oout=$( PATH="$otmp:$PATH" REPO=x/y bash "${BASH_SOURCE[0]}" owner 2>&1 || true )
  case "$oout" in
    *UNDETERMINED*) : ;;
    *) echo "SELF-TEST FAIL: with no blocker labelled and no verdict recorded, the owner queue did not say UNDETERMINED — silence would read as a green light to ship (got: $oout)" >&2; rc=1 ;;
  esac
  # MATCHED ON THE GREEN-LIGHT LINE, NOT ON A PHRASE THE WARNING ITSELF USES. The first version
  # forbade "ready to ship", which appears inside the UNDETERMINED warning as the thing it tells you
  # NOT to conclude — so the arm failed on correct output. A check whose corpus includes the text it
  # polices is the same mistake this file's siblings have made twice; here it failed loudly rather
  # than passing, which is the harmless direction.
  case "$oout" in
    *"No open blocker"*) echo "SELF-TEST FAIL: nobody having looked was reported as nothing blocking" >&2; rc=1 ;;
  esac

  # AND A LABELLED BLOCKER MUST BLOCK, BY NAME. A count alone is unfalsifiable from the output.
  cat > "$otmp/gh" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *"issues?state=open"*) echo '[{"number":67,"title":"a blocker","labels":[{"name":"blocks:release"}],"body":""}]' ;;
  *"issues/comments"*)   echo '[]' ;;
  *)                     echo '[]' ;;
esac
STUB
  oout=$( PATH="$otmp:$PATH" REPO=x/y bash "${BASH_SOURCE[0]}" owner 2>&1 || true )
  case "$oout" in
    *BLOCKED*) : ;;
    *) echo "SELF-TEST FAIL: an Issue labelled blocks:release did not block the release (got: $oout)" >&2; rc=1 ;;
  esac
  case "$oout" in
    *"#67"*) : ;;
    *) echo "SELF-TEST FAIL: the blocker was counted but not named — a count cannot be checked against the board" >&2; rc=1 ;;
  esac
  rm -rf "$otmp"

  [ "$rc" -eq 0 ] && echo "self-test passed: unknown roles refuse, every role has a queue, a failed lookup is not an empty queue, an unreviewed pull request is its author's work and nobody else's, a verdict on the current head settles it and one naming another head does not, a re-review goes back to the reviewer that already looked, a quoted verdict is not a verdict, three rounds of changes escalate to product and two do not, a failed review-history lookup offers nothing and says why, and the owner is told UNDETERMINED rather than being handed silence as a green light"
  return $rc
}

case "${1:-}" in
  --self-test) self_test ;;
  "") echo "usage: queue.sh <dev|qa|product|ops|pm> | --self-test" >&2; exit 2 ;;
  *) role_queue "$1" ;;
esac
