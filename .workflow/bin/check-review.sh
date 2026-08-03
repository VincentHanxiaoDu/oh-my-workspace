#!/usr/bin/env bash
# Gate 5 of 5: this head sha carries a review by an agent that authored none of its commits.
#
# Two questions survive the mechanical gates, and no test answers either: does this do what the
# Issue asked, and did it go wider than it should. That is what a review is for and the whole of it.
#
# Usage: check-review.sh <head-sha> <comments-json> <base-sha>
#        check-review.sh --self-test
set -euo pipefail

case "${1:-}" in
  -*) [ "$1" = "--self-test" ] || {
        echo "::error::unknown option '$1'. This is a typo, not an argument — refusing." >&2; exit 2; } ;;
esac

# THERE IS NO SUBSTANCE FLOOR MEASURED IN CHARACTERS. The previous build had one at 40 and it
# rejected an accurate 38-character scope statement while accepting 45 characters of "looks fine
# to me". A length is not a proxy for having looked. Emptiness is the only thing checkable here;
# everything else is the reviewer's judgement, and pretending otherwise moved the judgement into
# a number that could not hold it.
# THE POLICY IS PROJECT-OWNED, AND ITS ABSENCE MEANS THE STRICT RULE.
#
# `.workflow/review-policy` holds one word. `self-allowed` lets an author certify its own work; any
# other content, or no file at all, requires an independent verdict. It sits outside `.workflow/bin/`
# deliberately — bin/ is the framework's and is replaced wholesale on every install, and a policy a
# refresh silently reverts is not a policy.
#
# WHY THIS EXISTS. A pull request review means, by definition, somebody else approving. Some
# repositories have no somebody else: the framework's own repository is worked by one role, so the
# independence rule is unsatisfiable there by construction, and the only ways out were to bypass the
# gate as an admin or to abandon the record entirely. Both destroy the thing the gate is for.
#
# **A SELF-REVIEW IS PERMITTED AND IS NEVER SPELLED THE SAME WAY AS AN INDEPENDENT ONE.** It merges;
# it also says what it was, in the commit status, where it can be read and counted afterwards. That
# is the whole of the relaxation: a determined-but-weaker answer, kept distinguishable from the
# stronger one, which is this project's single rule applied to its own enforcement.
review_policy() {
  local f="${REVIEW_POLICY_FILE:-.workflow/review-policy}"
  [ -f "$f" ] || { echo independent; return 0; }
  local pv; pv=$(tr -d '[:space:]' < "$f" | tr 'A-Z' 'a-z')
  case "$pv" in
    self-allowed) echo self-allowed ;;
    # AN UNREADABLE OR MISSPELT POLICY IS THE STRICT ONE, NEVER THE PERMISSIVE ONE. A typo must not
    # silently widen what may merge, and `self_allowed` is a typo somebody will make.
    *) echo independent ;;
  esac
}

run_gate() {
  local head=$1 comments=$2 base=${3:-} rc=0 refused=0 authors reviewer verdict sha

  [ -f "$comments" ] || {
    echo "::error::'$comments' does not exist, so no review was examined. This is a LOOKUP FAILURE and NOT a statement that no review exists." >&2
    return 1
  }
  [ -n "$base" ] || {
    echo "::error::no base sha given, so the authors of this PR cannot be determined and independence cannot be judged. Refusing." >&2
    return 1
  }
  git rev-parse --verify --quiet "$base^{commit}" >/dev/null || {
    echo "::error::base commit '$base' is not in this clone. This is a CHECKOUT problem and NOT a verdict about the review." >&2
    return 1
  }

  # Who built it: the Agent: trailer of every commit in the range.
  # DERIVED BY pr-authors.sh, WHICH THE ROUTING ALSO CALLS. The rule includes one exemption — a
  # commit that changes nothing outside `openspec/` confers no authorship — and it is stated in
  # exactly one place on purpose. product must archive onto the branch before merging, so without
  # the exemption product authors every feature pull request and qa becomes the only role that can
  # ever certify one. That is a single point of failure by construction, and it deadlocked a board
  # of eleven. The exemption is earned by the diff and never by the subject line.
  authors=$("$(dirname "${BASH_SOURCE[0]}")/pr-authors.sh" --range "$base" HEAD)
  # TWO EMPTY SETS, TWO DIFFERENT FACTS. `authors` empty can mean the commits carry no trailer — a
  # defect, and not this gate's to diagnose — or that every commit was spec-only, in which case
  # nobody authored product judgement here and EVERY role is independent. The first build of the
  # exemption collapsed them and refused #63, an archive-only pull request, with "no commit carries
  # an Agent: trailer" about a commit carrying `Agent: product`. Unmergeable except with --admin.
  local trailers
  trailers=$("$(dirname "${BASH_SOURCE[0]}")/pr-authors.sh" --range "$base" HEAD --all-trailers)
  [ -n "$trailers" ] || {
    # SAY WHOSE PROBLEM THIS IS. This red is not about the review — it is about the commits, and a
    # reviewer reading "no independent review" concludes it is theirs to fix. The naming gate now
    # catches a missing trailer first and names the remedy; if this still fires, say plainly that
    # nothing here is a verdict on the review.
    echo "::error::no commit in $base..HEAD carries an 'Agent:' trailer, so who built this cannot be determined." >&2
    echo "  THIS IS NOT A VERDICT ON ANY REVIEW. It is a defect in the commits: add 'Agent: <role>'" >&2
    echo "  to each of them. The 'Branch name and commit convention' gate reports the same thing" >&2
    echo "  with the remedy, and it is the one to act on." >&2
    return 1
  }
  # Trailers exist and the author set is empty: every commit was spec-only. That is a DETERMINED
  # answer — nobody is an author — so the independence test below passes for whoever reviewed, and
  # this is not a refusal. An archive-only pull request still needs a review; it does not need an
  # impossible one.

  # The most recent review for THIS head sha. A push invalidates every prior review, which is why
  # the sha is part of the attestation rather than implied by it.
  #
  # A VERDICT IS A COMMENT, NOT A STRING FOUND INSIDE ONE (Issue #65). The previous build projected
  # `.body` out of the comment object and took the reviewer's name from the text — so a comment
  # QUOTING the verdict template read as a verdict, by whoever the quote happened to name. product
  # did it by accident on #63 while asking dev to re-attest, and it lost only because a genuine
  # verdict was posted afterwards and this selector takes `last`. Two things were wrong and both
  # are fixed here:
  #
  #   1. FENCED AND QUOTED TEXT IS DISCARDED BEFORE ANYTHING IS PARSED. `jq`'s `test()` had no
  #      notion of a code fence and `sed`'s `^` matches happily inside one. `strip_fences` removes
  #      ``` and ~~~ blocks; the selection is anchored to a line start so a `> `-quoted verdict is
  #      not one either. Only the fenced text goes — a reviewer that pastes command output into a
  #      real verdict still has a real verdict.
  #   2. THE REVIEWER IS THE POSTER, NOT THE NAME IN THE TEXT. `.role` is the `[role]` marker the
  #      roles already sign every comment with and that `queue.sh` already routes on, kept from the
  #      comment object rather than thrown away with it.
  #
  # `.user.login` IS DELIBERATELY NOT USED, and this is the load-bearing limitation. Every role on
  # this repository posts through the SAME GitHub account, so the login identifies the human and
  # would make all five roles one reviewer — turning the independence rule off entirely. The
  # `[role]` marker is what distinguishes them and it is a CONVENTION, NOT AN AUTHENTICATED FACT.
  # So: this closes the accident, and it does not make a verdict unforgeable by a role that sets
  # out to forge one. Anything stronger needs distinct posting identities, which is not this
  # script's to decide.
  #
  # EVERY VERDICT FOR THIS HEAD IS READ, NOT ONLY THE LAST (Issue #82). The previous build took
  # `| last` and computed everything from that one block, so an earlier `changes-requested` was
  # never looked at — and an author erased an independent refusal by posting a self-approve after
  # it, with no code change and no new commit. The outcome was byte-identical to there never having
  # been a refusal, down to publishing "NO INDEPENDENT AGENT HAS LOOKED AT THIS" while one had
  # looked and had said no. `11605b5` enabled self-review claiming it widened WHO may certify and
  # never WHAT counts as certified; that is the claim this restores.

  # ONE DEFINITION, USED BY BOTH jq PROGRAMS BELOW. They ask different questions of the same
  # comments and they must agree about what a quotation is: a fence honoured by one and not the
  # other would make a quoted verdict invisible to the selector and visible to the #84 scan, which
  # would turn every postmortem that pastes a bad sha into a red pull request.
  local JQ_STRIP_FENCES='
    def strip_fences:
      split("\n")
      | reduce .[] as $l ({inb:false, out:[]};
          if ($l | test("^[[:space:]]{0,3}(```|~~~)")) then .inb = (.inb | not)
          elif .inb then .
          else .out += [$l] end)
      | .out | join("\n");
'

  # ISSUE #84: A VERDICT THE GATE CANNOT PLACE IS REPORTED, NOT DISCARDED IN SILENCE.
  #
  # Matching `Reviewed-sha:` against the head EXACTLY is correct and is not touched here — it is
  # what makes a verdict stale the moment somebody pushes, and loosening it would let a review of
  # old code certify new code. The defect was the silence on one particular miss.
  #
  # TWO WAYS A VERDICT CAN NAME A SHA THAT IS NOT THE HEAD, AND THEY ARE DIFFERENT FACTS:
  #   1. the sha names a commit this repository KNOWS — an ordinary stale review. A push is
  #      expected to produce these, they need no announcement, and announcing them would bury the
  #      one that matters under noise on every branch that was ever pushed to. Unchanged: silent.
  #   2. the sha names NO OBJECT AT ALL — nothing was ever reviewed there, so this cannot be a
  #      stale review. Somebody posted a verdict that is not in force and nobody was told.
  #
  # Live on #38: a UAT refusal named e7e1368a7fbdd0c6ee7c07eebc0e5b6cf9d0e1b1 while the head was
  # e7e1368a36734b898b95803e95c23348e3718245 — EIGHT SHARED HEX CHARACTERS, which is not chance and
  # which everyone who read them side by side read as equal. The gate ran 28 seconds later and
  # published `success`. A role believed it had blocked that pull request; the board said green.
  # This is `could not determine` rendered as `nothing to see`, which is the one thing this project
  # says a check must never do.
  local unplaceable="" other_shas
  # THE POSTER IS CARRIED ALONGSIDE THE SHA, because the notice below has to say WHOSE verdict went
  # stale. A reviewer told only that "a verdict" was invalidated cannot tell whether it was theirs.
  other_shas=$(jq -r --arg h "$head" "$JQ_STRIP_FENCES"'
    [ .[]
      | ((.body // "") | gsub("\r"; "")) as $raw
      | ($raw | strip_fences | split("\n")) as $lines
      | select($lines | any(test("^Reviewed-by:")))
      | select($lines | any(test("^Verdict:")))
      | (($raw | split("\n")[0]
                | capture("^\\[(?<r>[A-Za-z][A-Za-z0-9_-]*)\\][[:space:]]*$") | .r) // "") as $role
      | $lines[]
      | select(test("^Reviewed-sha:"))
      | sub("^Reviewed-sha:[[:space:]]*"; "") | sub("[[:space:]]+$"; "") as $sha
      | select($sha != $h and $sha != "")
      | [$role, $sha]
    ] | unique | .[] | join("\u001f")' "$comments")

  if [ -n "$other_shas" ]; then
    # PROBE THE CHECKOUT, NEVER ASSUME IT. In a shallow clone an object can be absent because it
    # was not fetched, not because it does not exist — so "cat-file failed" would not mean
    # "unplaceable" and accusing on it would be a false alarm of exactly the kind this Issue is
    # about, pointed the other way. The review job checks out with fetch-depth: 0 today; that is a
    # property of a framework-owned workflow file and not something this script may take on trust.
    if [ "$(git rev-parse --is-shallow-repository 2>/dev/null || echo unknown)" != false ]; then
      echo "::notice::this clone is shallow or its depth could not be read, so whether any verdict names an unknown commit COULD NOT BE DETERMINED. That is not a finding that they are all fine (#84)." >&2
    else
      local orole osha
      while IFS=$'\037' read -r orole osha; do
        [ -n "$osha" ] || continue
        if git cat-file -e "${osha}^{commit}" 2>/dev/null; then
          # CASE 1: A STALE VERDICT, AND IT IS SAID OUT LOUD RATHER THAN LEFT AS AN ABSENCE.
          #
          # This exits nothing and changes no merge-eligibility. It exists because a stale verdict
          # and an absent one were byte-identical — both `no review found for head <X>`, both exit 1
          # — and they are opposite instructions: one is addressed to a reviewer who has already
          # looked and whose verdict raced a push, the other is addressed to a board where nobody
          # has looked. Rendering the first as the second is a determined-but-other state reported
          # as an absence, inside the machinery that exists to enforce that they are different.
          #
          # A NOTICE AND NOT AN ERROR, DELIBERATELY. Case 2 — a sha naming no object — is the loud
          # one, because it means somebody's verdict is not in force and never was. Announcing every
          # ordinary post-push staleness at the same volume would bury it, and a stale verdict is a
          # normal, expected consequence of pushing.
          if [ -n "$orole" ]; then
            echo "::notice::a verdict by '$orole' names $osha, which this repository knows but is not the head $head — it was invalidated by a push and needs re-posting against the current head." >&2
          else
            echo "::notice::a verdict names $osha, which this repository knows but is not the head $head — it was invalidated by a push and needs re-posting against the current head." >&2
          fi
        else
          case " $unplaceable " in
            *" $osha "*) : ;;
            *) unplaceable="${unplaceable}${osha} " ;;
          esac
        fi
      done <<UNPLACEABLE_SCAN
$other_shas
UNPLACEABLE_SCAN
      if [ -n "$unplaceable" ]; then
        echo "::error::a verdict on this pull request COULD NOT BE PLACED: it names sha(s) that are not the head and not any commit this repository knows — ${unplaceable% }" >&2
        echo "  The head this gate is checking is $head." >&2
        echo "  A verdict naming an unknown object is NOT a stale review: nothing was ever reviewed" >&2
        echo "  at that sha, so whoever posted it has a verdict that is not in force and was never" >&2
        echo "  told. A stale review — a sha this repository DOES know — is silent and is fine." >&2
        echo "  Remedy: re-post the verdict with the full 40-character head sha above. Exact-sha" >&2
        echo "  matching is deliberate and is not what is wrong here." >&2
      fi
    fi
  fi

  local records
  records=$(jq -r --arg h "$head" "$JQ_STRIP_FENCES"'
    def field($n):
      [ .[] | select(test("^" + $n + ":"))
            | sub("^" + $n + ":[[:space:]]*"; "") | sub("[[:space:]]+$"; "") ] | first // "";
    [ .[]
      | ((.body // "") | gsub("\r"; "")) as $raw
      | ($raw | strip_fences | split("\n")) as $lines
      | select($lines | any(test("^Reviewed-by:")))
      | select($lines | any(test("^Reviewed-sha:[[:space:]]*" + $h)))
      | { role: (($raw | split("\n")[0]
                       | capture("^\\[(?<r>[A-Za-z][A-Za-z0-9_-]*)\\][[:space:]]*$") | .r) // ""),
          declared: ($lines | field("Reviewed-by")),
          verdict:  ($lines | field("Verdict")) }
    ] | .[] | [.role, .declared, .verdict] | join("\u001f")' "$comments")

  [ -n "$records" ] || {
    # AN UNPLACEABLE VERDICT OUTRANKS "no review found" HERE TOO, and this early return is exactly
    # where it would have been lost: "no review exists" is true, and it sends the reader hunting
    # for a comment that is sitting right there naming a sha nobody can find.
    if [ -n "$unplaceable" ]; then return 4; fi
    echo "::error::no review found for head $head. A push invalidates any earlier review — this head needs its own." >&2
    echo "  A verdict QUOTED inside a code fence or a '>' block is not a verdict and is not counted (#65)." >&2
    return 1
  }

  # WHO POSTED IT IS ESTABLISHED BEFORE WHAT IT SAYS IS READ, FOR EVERY BLOCK. An unattributable
  # verdict is not a weak verdict, it is not a verdict — so this loop returns rather than setting rc
  # and carrying on. Its three refusals are three DIFFERENT facts and none of them is "no review
  # exists"; they share exit 1 with that only because all four mean this head is not certified.
  #
  # IT SWEEPS EVERY BLOCK RATHER THAN THE CERTIFYING ONE, and that is deliberate: a block that
  # cannot be attributed might be a refusal, and skipping over it to reach a later approve is the
  # #82 defect wearing a different hat. This is bounded — it only ever looks at verdicts naming THIS
  # head, so a push clears it and the remedy is named in the message.
  local role declared verdict_
  # US (0x1f), NOT A TAB. A tab is IFS whitespace, so `read` strips a LEADING EMPTY field and an
  # unattributable block — role empty — shifted every field left and was reported as a
  # disagreement rather than as undetermined. The script's own arm 7e caught it.
  while IFS=$'\037' read -r role declared verdict_; do
    [ -n "$role" ] || {
      echo "::error::a review block for $head was found, but WHO POSTED IT COULD NOT BE DETERMINED, so it certifies nothing. REFUSING." >&2
      echo "  THIS IS NOT A FINDING THAT THE REVIEW IS ABSENT OR FORGED. It is an inability to attribute it." >&2
      echo "  The remedy is the convention every role already follows: '[<role>]' ALONE on the comment's" >&2
      echo "  very first line, above the Reviewed-by:/Reviewed-sha:/Verdict: block. Re-post it and this clears." >&2
      echo "  The name inside the block is NOT read as a fallback: that is exactly the hole #65 is about." >&2
      return 1
    }
    [ -n "$declared" ] || { echo "::error::the review names no reviewer" >&2; return 1; }
    if [ "$role" != "$declared" ]; then
      # NOT SILENTLY RE-ATTRIBUTED TO THE POSTER. Quietly correcting the name would let an attempt to
      # certify somebody else's work — or one's own under another name — pass unremarked, and the
      # attempt is the thing worth seeing.
      echo "::error::this verdict was posted by '[$role]' but declares 'Reviewed-by: $declared'. THE TWO DISAGREE, so it is REFUSED — not re-attributed to either of them." >&2
      echo "  Its Verdict: line was not acted on at all, because a verdict whose author is in doubt is not a verdict." >&2
      echo "  If '[$role]' wrote this review, correct the Reviewed-by: line to '$role' and re-post." >&2
      return 1
    fi
    case "$verdict_" in
      approve|changes-requested) : ;;
      "") echo "::error::the review by '$role' carries no Verdict:" >&2; rc=1 ;;
      *) echo "::error::unknown verdict '$verdict_' from '$role' — expected approve or changes-requested" >&2; rc=1 ;;
    esac
  done <<EOF
$records
EOF

  # A REFUSAL IS CLEARED ONLY BY A LATER VERDICT FROM THE SAME REVIEWER. Each reviewer's most
  # recent verdict for this head is kept, and the gate refuses while ANY of them requests changes.
  #
  # WHY THIS RULE AND NOT ANOTHER. "A reviewer changed its mind" is the only thing that should
  # retire that reviewer's refusal, and it must stay possible or a refused branch could never be
  # cleared by the reviewer that refused it. Nobody else gets a vote on it: not the author, which is
  # #82 itself, and not a second independent reviewer, which #82 records as the pre-existing half of
  # the same defect and which is the same act — overriding somebody else's judgement by posting
  # after them. Two reviewers who disagree resolve it the way people do, and the one who refused
  # withdraws. THE ESCAPE IS THE PUSH: a verdict is bound to a head sha, so fixing the code makes a
  # new head and every verdict above stops applying. A refused branch is never trapped; it is fixed.
  local refusers
  refusers=$(printf '%s\n' "$records" | awk -F'\037' '
    { latest[$1] = $3; seen[$1] = 1 }
    END { for (r in seen) if (latest[r] == "changes-requested") print r }' | sort | tr '\n' ' ' | sed 's/[[:space:]]*$//')
  if [ -n "$refusers" ]; then
    # EXIT 2, NOT 1. A refused review and an absent one are different facts, and they shared an
    # exit code — so the workflow could only publish one description for both, and a reviewer that
    # had just refused a pull request read "No current review by an independent agent" and could not
    # tell its verdict had landed from its comment never being parsed. Caught by a reviewer that
    # checked the fix rather than the claim; the previous attempt grepped a log file and did not work.
    #
    # AND IT NAMES WHO REFUSED. "changes were requested" alone sends the author reading every
    # comment on the pull request to find out whose objection is outstanding.
    echo "::error::the current review requests changes — outstanding refusal(s) by: $refusers" >&2
    echo "  A LATER APPROVE DOES NOT CLEAR THIS (#82). Only a new verdict from the same reviewer" >&2
    echo "  retires its refusal, or a push, which makes a new head that these verdicts do not name." >&2
    refused=1
  fi

  # The certifying verdict is the most recent one, as before. It decides WHO is being checked for
  # independence; it no longer decides whether anything was refused.
  local last_record
  last_record=$(printf '%s\n' "$records" | tail -1)
  reviewer=$(printf '%s' "$last_record" | cut -d$'\037' -f1)
  verdict=$(printf '%s' "$last_record" | cut -d$'\037' -f3)

  # INDEPENDENCE. An agent that wrote any commit in this range cannot certify it — including the pm.
  if printf '%s\n' "$authors" | grep -qx "$reviewer"; then
    # `refused` IS TESTED HERE AS WELL AS `rc`, AND IT IS DEFENCE IN DEPTH RATHER THAN THE ONLY
    # DEFENCE — said plainly, because a comment claiming a test it does not have is worse than no
    # comment. A `changes-requested` verdict records itself in `refused` and only becomes rc=2
    # further down, which is after this; that conversion wins, so removing this clause does not
    # currently change any outcome and arm 5e stays green under that mutation.
    #
    # #82 MADE IT MATTER MORE WITHOUT MAKING IT LOAD-BEARING. `refused` can now be set by a
    # DIFFERENT reviewer's outstanding refusal while the certifying verdict is the author's
    # self-approve — which is the whole of #82 — so this clause is now the difference between
    # reaching rc=3 and rc=1 on that path. The conversion below still wins either way and still
    # makes it 2.
    #
    # It earned its place all the same: the first version of this block `return`ed as soon as it set
    # rc=3, which skipped the conversion entirely and DID turn a refusal into a pass. Arm 5e caught
    # that one. The early return is gone; the guard stays, so reintroducing it cannot resurrect the
    # bug. **Widening WHO may certify must never widen WHAT counts as certified.**
    if [ "$(review_policy)" = self-allowed ] && [ "$rc" -eq 0 ] && [ "${refused:-0}" -eq 0 ]; then
      # EXIT 3: SELF-REVIEWED. Not 0, which would say an independent agent looked, and not 1, which
      # would say nobody did. A third fact needs a third code, for exactly the reason a refusal
      # needed one — the publishing step picks its wording from this number, and two facts sharing
      # a code is how a landed refusal came to be published as an absent review.
      echo "::notice::'$reviewer' authored commits in this PR. This repository permits a self-review, so this PASSES — published as a SELF-review, never as an independent one." >&2
      rc=3
    else
      echo "::error::'$reviewer' authored commits in this PR, so its review does not establish independence" >&2
      rc=1
    fi
  fi

  # A REFUSAL SURVIVES EVERY OTHER COMPLAINT ABOUT THE SAME REVIEW, and this is the line that makes
  # it so. `rc` used to be one scalar that each check overwrote in turn, so a `changes-requested`
  # set 2 and the independence check three lines later set 1 — and the workflow, reading the exit
  # code to choose its wording, published "No current review by an independent agent" over a
  # verdict that had landed and refused. That is exactly the confusion the exit code was split to
  # end, reintroduced from the other side. Driven on PR #42: the log carried BOTH errors and the
  # process exited 1. The refusal is the fact the reviewer needs back, so it is the one that sets
  # the code; the other errors still print, and nothing is certified that was not certified before
  # — 2 is a failure like 1 is.
  # `if`, not `[ … ] && rc=2`: under `set -e` a bare AND-list whose test fails is a failing
  # statement, and the good-approve path (refused=0) would abort the script instead of returning 0.
  if [ "$refused" -eq 1 ]; then rc=2; fi

  # EXIT 4: A VERDICT COULD NOT BE PLACED (#84). A fourth fact needs a fourth code, for the same
  # reason a refusal needed its own — the publishing step picks its wording from this number, and
  # two facts sharing a code is how a landed refusal came to be published as an absent review.
  #
  # IT MUST NOT PASS, AND THAT IS MORE THAN REPORTING. The Issue's remedy asks for the miss to be
  # REPORTED; reporting it into the log while still publishing `success` would leave #38 exactly as
  # it is — green, merge-eligible, with a role believing it had blocked it — and the harm the Issue
  # names is the merge-eligibility, not the quietness on its own.
  #
  # PRECEDENCE, STATED RATHER THAN LEFT TO THE ORDER OF LINES. 4 beats 0 and 1; it LOSES to 2.
  #   - over 0: this is the whole defect. A pass while somebody's verdict lies unplaced is the thing.
  #   - over 1: "no review exists" is true but sends the reader hunting for a comment that is in
  #     fact sitting right there naming a sha nobody can find. The unplaceable one is actionable.
  #   - under 2: a landed refusal is concrete, already red, and already tells the author what to do.
  #     The unplaceable notice still prints above it, so nothing is lost by not owning the code.
  if [ -n "$unplaceable" ] && [ "$rc" -ne 2 ]; then rc=4; fi

  [ "$rc" -eq 0 ] && echo "review ok: $head reviewed by '$reviewer', which authored none of its commits"
  [ "$rc" -eq 3 ] && echo "review ok (SELF-REVIEWED): $head certified by '$reviewer', which also built it. NO INDEPENDENT AGENT HAS LOOKED AT THIS."
  return "$rc"
}

self_test() {
  local tmp rc=0 out me
  me=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' RETURN

  git -C "$tmp" init -q -b main 2>/dev/null || { mkdir -p "$tmp"; git -C "$tmp" init -q -b main; }
  git -C "$tmp" config user.email t@t; git -C "$tmp" config user.name t
  echo x > "$tmp/f"; git -C "$tmp" add -A; git -C "$tmp" commit -qm "chore: seed"
  local base; base=$(git -C "$tmp" rev-parse HEAD)
  echo y > "$tmp/g"; git -C "$tmp" add -A
  git -C "$tmp" commit -qm "fix(x): y

Agent: dev-a"
  local head; head=$(git -C "$tmp" rev-parse HEAD)

  # EVERY VERDICT IS POSTED BY SOMEBODY (#65), so every fixture below carries the `[role]` marker
  # that says who. `_cp` sets the poster explicitly — that is how the disagreement cases are
  # driven. `_c` is the honest case and derives the marker from the name the block declares, so the
  # arms that are about something else read as they did before.
  _cp() { printf '[{"body":"[%s]\\n%s"}]' "$1" "$2" > "$tmp/c.json"; }
  _c() { _cp "$(printf '%s' "$1" | sed -n 's/.*Reviewed-by:[[:space:]]*\([A-Za-z0-9_-]*\).*/\1/p')" "$1"; }
  _run() { ( cd "$tmp" && bash "$me" "$head" "$tmp/c.json" "$base" ) 2>&1; }

  # 1. A MISSING COMMENTS FILE MUST REFUSE — not report that no review exists. Those are different
  #    values and the whole project turns on the difference.
  out=$( cd "$tmp" && bash "$me" "$head" "$tmp/absent.json" "$base" 2>&1 ) \
    && { echo "SELF-TEST FAIL: a missing comments file PASSED" >&2; rc=1; }
  case "$out" in
    *"LOOKUP FAILURE"*) : ;;
    *) echo "SELF-TEST FAIL: a missing comments file was not distinguished from an absent review" >&2; rc=1 ;;
  esac

  # 2. An independent approve for this head must PASS.
  _c "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve"
  _run >/dev/null || { echo "SELF-TEST FAIL: an independent approve was rejected" >&2; rc=1; }

  # 3. A REVIEW BY THE AUTHOR MUST FAIL. This is the rule the other four gates depend on.
  _c "Reviewed-by: dev-a\\nReviewed-sha: $head\\nVerdict: approve"
  _run >/dev/null 2>&1 && { echo "SELF-TEST FAIL: an author reviewing its own work PASSED" >&2; rc=1; }

  # 4. A review of a DIFFERENT sha must not certify this head — a push invalidates a review.
  #    IT NAMES `$base`, A REAL EARLIER COMMIT, AND NOT AN ALL-ZEROS STRING. That is what a stale
  #    review actually looks like, and since #84 the two are different facts: a sha this repository
  #    knows is silently stale, a sha naming no object at all is reported. Arm 5i(e) drives the latter.
  _c "Reviewed-by: reviewer-a\\nReviewed-sha: $base\\nVerdict: approve"
  _run >/dev/null 2>&1 && { echo "SELF-TEST FAIL: a review of another sha certified this head" >&2; rc=1; }

  # 5. changes-requested must FAIL, AND WITH ITS OWN EXIT CODE. Sharing one with "no review at all"
  #    is why a reviewer could not tell a landed refusal from an unparsed comment.
  _c "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: changes-requested"
  local crc=0; _run >/dev/null 2>&1 || crc=$?
  [ "$crc" -eq 2 ] || { echo "SELF-TEST FAIL: changes-requested exited $crc, not 2 — it shares a code with an absent review" >&2; rc=1; }
  # And an ABSENT review must NOT use that code.
  _c "Reviewed-by: reviewer-a\\nReviewed-sha: $base\\nVerdict: approve"
  local arc=0; _run >/dev/null 2>&1 || arc=$?
  [ "$arc" -eq 1 ] || { echo "SELF-TEST FAIL: an absent review exited $arc, not 1" >&2; rc=1; }

  # 5b. A REFUSAL BY A NON-INDEPENDENT REVIEWER STILL EXITS 2. Case 5 only ever exercised a refusal
  #     with nothing else wrong with it, so it stayed green while `rc` was a single scalar that the
  #     independence check overwrote — and PR #42, whose refusal came from an agent that had also
  #     committed, exited 1 and was published as "No current review by an independent agent". Two
  #     complaints about one review; the refusal is the one the reviewer needs back.
  _c "Reviewed-by: dev-a\\nReviewed-sha: $head\\nVerdict: changes-requested"
  local nrc=0; _run >/dev/null 2>&1 || nrc=$?
  [ "$nrc" -eq 2 ] || { echo "SELF-TEST FAIL: a refusal by a non-independent reviewer exited $nrc, not 2 — a landed refusal reads as no review at all" >&2; rc=1; }

  # 5c. AN ARCHIVE-ONLY PULL REQUEST MUST BE REVIEWABLE. Every commit spec-only means nobody
  #     authored product judgement, so any reviewer is independent — it does NOT mean the commits
  #     carry no trailer. #63 was refused with "no commit carries an Agent: trailer" about a commit
  #     carrying `Agent: product`, and could not be merged except with --admin. Driven through the
  #     real entry point on a branch whose only commit touches nothing outside openspec/.
  local sp; sp=$(mktemp -d)
  git -C "$sp" init -q -b main; git -C "$sp" config user.email t@t; git -C "$sp" config user.name t
  mkdir -p "$sp/openspec/specs/x"
  echo seed > "$sp/f"; git -C "$sp" add -A; git -C "$sp" commit -qm "chore: seed"
  local sbase; sbase=$(git -C "$sp" rev-parse HEAD)
  echo spec > "$sp/openspec/specs/x/spec.md"; git -C "$sp" add -A
  git -C "$sp" commit -qm "chore(x): archive what shipped

Agent: product"
  local shead; shead=$(git -C "$sp" rev-parse HEAD)
  cp "$(dirname "$me")/pr-authors.sh" "$sp/pr-authors.sh" 2>/dev/null || true
  cp "$me" "$sp/check-review.sh"
  printf '[{"body":"[qa]\\nReviewed-by: qa\\nReviewed-sha: %s\\nVerdict: approve"}]' "$shead" > "$sp/c.json"
  local src=0
  ( cd "$sp" && bash "$sp/check-review.sh" "$shead" "$sp/c.json" "$sbase" ) >/dev/null 2>&1 || src=$?
  [ "$src" -eq 0 ] || {
    echo "SELF-TEST FAIL: an archive-only pull request with a clean independent approve exited $src — nobody authored it, so nobody can be non-independent, and refusing it makes it unmergeable without --admin" >&2; rc=1; }
  rm -rf "$sp"

  # 5d. THE SELF-REVIEW POLICY, DRIVEN IN ALL THREE DIRECTIONS. A repository with one agent cannot
  #     satisfy the independence rule at all, and the two escapes available before this — bypass the
  #     gate as an admin, or stop requiring a review — destroy exactly what the gate is for.
  local pdir; pdir=$(mktemp -d)
  _c "Reviewed-by: dev-a\\nReviewed-sha: $head\\nVerdict: approve"

  #     (a) DEFAULT IS STRICT. No policy file means an author still cannot certify its own work; a
  #         relaxation that arrives by default is one nobody chose.
  local drc=0
  ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/absent" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || drc=$?
  [ "$drc" -eq 1 ] || { echo "SELF-TEST FAIL: with no policy file a self-review exited $drc, not 1 — the strict rule must be the default" >&2; rc=1; }

  #     (b) A TYPO IS STRICT TOO. `self_allowed` is a spelling somebody will use, and a misread
  #         policy must never be the permissive one.
  printf 'self_allowed\n' > "$pdir/typo"
  drc=0
  ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/typo" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || drc=$?
  [ "$drc" -eq 1 ] || { echo "SELF-TEST FAIL: a misspelt policy exited $drc, not 1 — a typo silently widened what may merge" >&2; rc=1; }

  #     (c) `self-allowed` PASSES WITH ITS OWN CODE. 3, not 0: a caller that cannot tell a
  #         self-review from an independent one will publish them with the same sentence, and then
  #         nobody can count what shipped uncertified.
  printf 'self-allowed\n' > "$pdir/ok"
  drc=0
  ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || drc=$?
  [ "$drc" -eq 3 ] || { echo "SELF-TEST FAIL: a permitted self-review exited $drc, not 3 — it must pass, and it must not pass as an independent review" >&2; rc=1; }

  #     (d) AND IT SAYS SO. The status wording is derived from this output and the exit code; if the
  #         success line claims independence, the record is wrong in the one place anyone reads it.
  local sout
  sout=$( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" 2>&1 || true )
  case "$sout" in
    *SELF-REVIEWED*) : ;;
    *) echo "SELF-TEST FAIL: a self-review did not announce itself as one (got: $sout)" >&2; rc=1 ;;
  esac
  case "$sout" in
    *"authored none of its commits"*) echo "SELF-TEST FAIL: a self-review claimed the reviewer authored none of its commits" >&2; rc=1 ;;
  esac

  #     (e) THE POLICY DOES NOT FORGIVE A REFUSAL. `changes-requested` from an author is still a
  #         refusal; self-review widens WHO may certify, never WHAT counts as certified.
  _c "Reviewed-by: dev-a\\nReviewed-sha: $head\\nVerdict: changes-requested"
  drc=0
  ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || drc=$?
  [ "$drc" -eq 2 ] || { echo "SELF-TEST FAIL: a self-review requesting changes exited $drc, not 2 — the policy turned a refusal into a pass" >&2; rc=1; }

  #     (f) ISSUE #82: A SELF-APPROVE DOES NOT ERASE AN INDEPENDENT REFUSAL. 5e covers a refusal that
  #         is the ONLY verdict; this covers a refusal that a later self-approve tries to bury. The
  #         gate used to select one block with `| last`, so the earlier refusal was never read and
  #         the outcome was byte-identical to there never having been one — including publishing
  #         "NO INDEPENDENT AGENT HAS LOOKED AT THIS" while one had looked and had said no.
  #         BOTH CONTROLS ARE HERE, because the defect's signature is the test agreeing with the
  #         no-refusal control.
  local ctl=0
  #         CONTROL: an independent refusal alone refuses with 2.
  _cp reviewer-a "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: changes-requested"
  ctl=0; ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || ctl=$?
  [ "$ctl" -eq 2 ] || { echo "SELF-TEST FAIL: control — an independent refusal alone exited $ctl, not 2, so the #82 arms below prove nothing" >&2; rc=1; }
  #         CONTROL: a self-approve alone passes with 3.
  _cp dev-a "Reviewed-by: dev-a\\nReviewed-sha: $head\\nVerdict: approve"
  ctl=0; ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || ctl=$?
  [ "$ctl" -eq 3 ] || { echo "SELF-TEST FAIL: control — a self-approve alone exited $ctl, not 3" >&2; rc=1; }
  #         TEST: the refusal first, then the author's self-approve. Must still be 2.
  printf '[{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: changes-requested"},{"body":"[dev-a]\\nReviewed-by: dev-a\\nReviewed-sha: %s\\nVerdict: approve"}]' "$head" "$head" > "$tmp/c.json"
  ctl=0; local cout
  cout=$( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" 2>&1 ) || ctl=$?
  [ "$ctl" -eq 2 ] || { echo "SELF-TEST FAIL: an author erased an independent refusal with a self-approve (exit $ctl, want 2)" >&2; rc=1; }
  case "$cout" in
    *SELF-REVIEWED*) echo "SELF-TEST FAIL: a head carrying an outstanding refusal published as SELF-REVIEWED" >&2; rc=1 ;;
  esac
  case "$cout" in
    *reviewer-a*) : ;;
    *) echo "SELF-TEST FAIL: the refusal did not name who refused (got: $cout)" >&2; rc=1 ;;
  esac

  #     (g) A SECOND INDEPENDENT REVIEWER CANNOT VOTE AWAY THE FIRST ONE'S REFUSAL EITHER. #82
  #         records this as the pre-existing half of the same defect; it is the same act.
  printf '[{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: changes-requested"},{"body":"[reviewer-b]\\nReviewed-by: reviewer-b\\nReviewed-sha: %s\\nVerdict: approve"}]' "$head" "$head" > "$tmp/c.json"
  ctl=0; ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 || ctl=$?
  [ "$ctl" -eq 2 ] || { echo "SELF-TEST FAIL: a second reviewer voted away the first reviewer's refusal (exit $ctl, want 2)" >&2; rc=1; }

  #     (h) AND A REVIEWER MAY WITHDRAW ITS OWN REFUSAL. This is the direction that keeps a refused
  #         branch clearable; without it the fix above traps every refused pull request forever.
  printf '[{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: changes-requested"},{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: approve"}]' "$head" "$head" > "$tmp/c.json"
  ( cd "$tmp" && REVIEW_POLICY_FILE="$pdir/ok" bash "$me" "$head" "$tmp/c.json" "$base" ) >/dev/null 2>&1 \
    || { echo "SELF-TEST FAIL: a reviewer could not withdraw its own refusal, so a refused branch can never be cleared" >&2; rc=1; }

  rm -rf "$pdir"

  # 5i. ISSUE #84: A VERDICT NAMING A SHA THIS REPOSITORY DOES NOT KNOW IS REPORTED, NOT DISCARDED
  #     IN SILENCE. On #38 a UAT refusal named a sha sharing EIGHT hex characters with the head and
  #     naming no object at all; the gate published `success` 28 seconds later. Exact-sha matching
  #     is correct and untouched — the silence on one particular miss was the defect.
  #
  #     The ghost shares the head's first eight characters ON PURPOSE. That collision is what made
  #     the live case dangerous rather than obvious, and a fixture whose two shas differ at a glance
  #     would not represent it.
  local ghost="${head:0:8}$(printf '%s' "${head:8}" | tr '0-9a-f' '5678943210fedcba')"
  #     CONTROLS FIRST: the head must resolve and the ghost must not, or the arms below prove
  #     nothing. This is the check the Issue itself ran.
  ( cd "$tmp" && git cat-file -e "$head^{commit}" ) 2>/dev/null \
    || { echo "SELF-TEST FAIL: control — the fixture head does not resolve, so the #84 arms determined nothing" >&2; rc=1; }
  ( cd "$tmp" && git cat-file -e "$ghost^{commit}" ) 2>/dev/null \
    && { echo "SELF-TEST FAIL: control — the constructed ghost sha resolves, so it is not an unknown object" >&2; rc=1; }

  #     (a) THE #38 SHAPE: an unplaceable refusal alongside a genuine approve. Used to exit 0.
  printf '[{"body":"[product]\\nReviewed-by: product\\nReviewed-sha: %s\\nVerdict: changes-requested"},{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: approve"}]' "$ghost" "$head" > "$tmp/c.json"
  local urc=0 uout
  uout=$(_run) || urc=$?
  [ "$urc" -eq 4 ] || { echo "SELF-TEST FAIL: a verdict naming an unknown sha exited $urc, not 4 — it was discarded in silence and a genuine approve published a pass over it" >&2; rc=1; }
  case "$uout" in
    *"$ghost"*) : ;;
    *) echo "SELF-TEST FAIL: the unplaceable verdict was not reported with the sha it could not place (got: $uout)" >&2; rc=1 ;;
  esac
  case "$uout" in
    *"$head"*) : ;;
    *) echo "SELF-TEST FAIL: the report did not name the head it expected (got: $uout)" >&2; rc=1 ;;
  esac

  #     (b) AN ORDINARY STALE REVIEW STAYS SILENT. `$base` is a commit this repository KNOWS and is
  #         not the head. A push is expected to stale reviews; announcing those would bury (a)
  #         under noise on every branch that was ever pushed to.
  printf '[{"body":"[product]\\nReviewed-by: product\\nReviewed-sha: %s\\nVerdict: changes-requested"},{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: approve"}]' "$base" "$head" > "$tmp/c.json"
  urc=0; uout=$(_run) || urc=$?
  [ "$urc" -eq 0 ] || { echo "SELF-TEST FAIL: an ordinary stale review (a sha this repository knows) exited $urc, not 0" >&2; rc=1; }
  case "$uout" in
    *"COULD NOT BE PLACED"*) echo "SELF-TEST FAIL: an ordinary stale review was reported as unplaceable" >&2; rc=1 ;;
  esac

  #     (c) A QUOTED unplaceable verdict is not a verdict, so it is not an unplaceable one either —
  #         or every postmortem that pastes a bad sha turns a pull request red.
  printf '[{"body":"[product]\\nwhat went wrong was:\\n```\\nReviewed-by: product\\nReviewed-sha: %s\\nVerdict: changes-requested\\n```"},{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: approve"}]' "$ghost" "$head" > "$tmp/c.json"
  urc=0; uout=$(_run) || urc=$?
  [ "$urc" -eq 0 ] || { echo "SELF-TEST FAIL: a QUOTED verdict naming an unknown sha exited $urc, not 0 — a quotation was treated as a verdict" >&2; rc=1; }

  #     (d) A LANDED REFUSAL OUTRANKS IT, and both are said. 2 is the concrete, already-actionable
  #         fact; the unplaceable notice still prints.
  printf '[{"body":"[product]\\nReviewed-by: product\\nReviewed-sha: %s\\nVerdict: approve"},{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: changes-requested"}]' "$ghost" "$head" > "$tmp/c.json"
  urc=0; uout=$(_run) || urc=$?
  [ "$urc" -eq 2 ] || { echo "SELF-TEST FAIL: a landed refusal alongside an unplaceable verdict exited $urc, not 2" >&2; rc=1; }
  case "$uout" in
    *"COULD NOT BE PLACED"*) : ;;
    *) echo "SELF-TEST FAIL: the unplaceable verdict went unmentioned because a refusal outranked it" >&2; rc=1 ;;
  esac

  #     (f) A STALE VERDICT AND AN ABSENT ONE MUST NOT RENDER THE SAME SENTENCE. Both exit 1 —
  #         neither certifies this head — so the distinction lives entirely in what is said. They
  #         were byte-identical: `no review found for head <X>` for both. One means "your verdict
  #         raced a push, re-post it" and is addressed to a reviewer who has already looked; the
  #         other means "nobody has looked". Opposite instructions from one sentence.
  printf '[{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: changes-requested"}]' "$base" > "$tmp/c.json"
  local stale_out; urc=0
  stale_out=$(_run) || urc=$?
  [ "$urc" -eq 1 ] || { echo "SELF-TEST FAIL: a stale verdict exited $urc, not 1 — this must not move any exit code" >&2; rc=1; }
  printf '[]' > "$tmp/c.json"
  local absent_out; urc=0
  absent_out=$(_run) || urc=$?
  [ "$urc" -eq 1 ] || { echo "SELF-TEST FAIL: an absent review exited $urc, not 1" >&2; rc=1; }
  [ "$stale_out" != "$absent_out" ] || { echo "SELF-TEST FAIL: a stale verdict and an absent review publish the identical output — a reviewer whose verdict raced a push reads it as 'nobody has looked'" >&2; rc=1; }
  case "$stale_out" in
    *"reviewer-a"*) : ;;
    *) echo "SELF-TEST FAIL: the stale-verdict notice does not say WHOSE verdict went stale (got: $stale_out)" >&2; rc=1 ;;
  esac
  case "$stale_out" in
    *"$base"*) : ;;
    *) echo "SELF-TEST FAIL: the stale-verdict notice does not name the sha that went stale (got: $stale_out)" >&2; rc=1 ;;
  esac
  case "$stale_out" in
    #         A NOTICE, NOT AN ERROR. Case 2 is the loud one; announcing every ordinary post-push
    #         staleness at the same volume would bury it.
    *"::notice::"*) : ;;
    *) echo "SELF-TEST FAIL: the stale verdict was not reported as a ::notice:: (got: $stale_out)" >&2; rc=1 ;;
  esac

  #     (g) AND A VERDICT NAMING THE CORRECT HEAD SAYS NOTHING AT ALL. The ordinary path stays
  #         silent, or every green pull request grows a paragraph nobody reads.
  _cp reviewer-a "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve"
  local quiet_out; quiet_out=$(_run 2>&1) || true
  case "$quiet_out" in
    *"::notice::"*|*"::error::"*) echo "SELF-TEST FAIL: a verdict naming the correct head produced a notice or an error (got: $quiet_out)" >&2; rc=1 ;;
  esac

  #     (e) AN ALL-ZEROS SHA IS UNPLACEABLE, NOT STALE — and this is a BEHAVIOUR CHANGE worth
  #         pinning rather than discovering. Arms 4 and 5 used to spell "some other head" as forty
  #         zeros, which named no object and so, since #84, is reported rather than passed over.
  #         They now name `$base`, a real earlier commit. Nobody reviewed anything at all-zeros.
  printf '[{"body":"[reviewer-a]\\nReviewed-by: reviewer-a\\nReviewed-sha: 0000000000000000000000000000000000000000\\nVerdict: approve"}]' > "$tmp/c.json"
  urc=0; _run >/dev/null 2>&1 || urc=$?
  [ "$urc" -eq 4 ] || { echo "SELF-TEST FAIL: a verdict naming an all-zeros sha exited $urc, not 4 — it names no object, so it is unplaceable and not merely stale" >&2; rc=1; }

  # 6. AN ACCURATE SHORT REVIEW MUST PASS. The previous build's character floor rejected a
  #    38-character scope statement and accepted 45 characters of "looks fine to me".
  _c "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve\\nScope: one file, as asked."
  _run >/dev/null || { echo "SELF-TEST FAIL: a short but accurate review was rejected" >&2; rc=1; }

  # 7. ISSUE #65: A QUOTED VERDICT IS NOT A VERDICT, AND A VERDICT IS ITS POSTER'S.
  #    The gate used to take the reviewer's name from the comment TEXT, so any role could mint any
  #    other role's approval — and product did it by accident on #63, quoting the template to ASK
  #    for a verdict. `reviewer-a` authored nothing in this range, so each forgery below would have
  #    come out as a clean independent approve and exited 0.

  #    (a) THE #63 NEAR-MISS. A fenced quote inside somebody else's prose.
  _cp product "please re-attest:\\n\\n\`\`\`\\nReviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve\\n\`\`\`\\n\\nthanks"
  local qrc=0; _run >/dev/null 2>&1 || qrc=$?
  [ "$qrc" -eq 1 ] || { echo "SELF-TEST FAIL: a verdict QUOTED inside a code fence exited $qrc, not 1 — a request for a review was counted as one" >&2; rc=1; }

  #    (b) `~~~` IS A FENCE TOO. A fix that only knew about backticks would be a fix for the single
  #        comment that caused the incident.
  _cp product "example:\\n~~~\\nReviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve\\n~~~"
  qrc=0; _run >/dev/null 2>&1 || qrc=$?
  [ "$qrc" -eq 1 ] || { echo "SELF-TEST FAIL: a verdict quoted inside a ~~~ fence exited $qrc, not 1" >&2; rc=1; }

  #    (c) A GENUINE VERDICT MAY STILL CONTAIN A FENCE. Reviewers paste what they ran. Stripping
  #        the fence must not discard the comment — a fix that refuses everything passes (a) and
  #        (b) and breaks the workflow entirely.
  _cp reviewer-a "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve\\n\\nI ran it:\\n\`\`\`\\n\$ make ci\\nok\\n\`\`\`"
  _run >/dev/null || { echo "SELF-TEST FAIL: a genuine verdict that also quotes command output was rejected" >&2; rc=1; }

  #    (d) A NAME THAT DISAGREES WITH ITS POSTER IS REFUSED, NOT RE-ATTRIBUTED. Silent correction
  #        would hide the attempt, and the attempt is the thing worth seeing. Driven with the
  #        AUTHOR as poster: `dev-a` built this branch, and typing another role's name was the
  #        whole of what certifying its own work used to take.
  _cp dev-a "Reviewed-by: reviewer-a\\nReviewed-sha: $head\\nVerdict: approve"
  qrc=0; local qout; qout=$(_run) || qrc=$?
  [ "$qrc" -eq 1 ] || { echo "SELF-TEST FAIL: an author certified its own work by typing another role's name (exit $qrc)" >&2; rc=1; }
  case "$qout" in
    *DISAGREE*) : ;;
    *) echo "SELF-TEST FAIL: a verdict whose declared reviewer differs from its poster was refused without saying they disagree (got: $qout)" >&2; rc=1 ;;
  esac

  #    (e) AN UNATTRIBUTABLE VERDICT SAYS SO, AND DOES NOT FALL BACK TO THE DECLARED NAME. Reading
  #        `Reviewed-by:` when there is no poster to check it against is precisely the hole, so a
  #        comment with no `[role]` marker refuses — as UNDETERMINED, which is a different value
  #        from "no review exists" and must not be spelled like it.
  printf '[{"body":"Reviewed-by: reviewer-a\\nReviewed-sha: %s\\nVerdict: approve"}]' "$head" > "$tmp/c.json"
  qrc=0; qout=$(_run) || qrc=$?
  [ "$qrc" -eq 1 ] || { echo "SELF-TEST FAIL: a verdict with no [role] marker exited $qrc, not 1 — it was attributed to a name it declared about itself" >&2; rc=1; }
  case "$qout" in
    *"COULD NOT BE DETERMINED"*) : ;;
    *) echo "SELF-TEST FAIL: an unattributable verdict was not distinguished from an absent one (got: $qout)" >&2; rc=1 ;;
  esac

  [ "$rc" -eq 0 ] && echo "self-test passed: a missing file refuses, an author cannot certify its own work, a stale sha does not carry over, a short accurate review passes, a QUOTED verdict is not a verdict, a verdict that names somebody other than its poster is refused, a landed refusal survives every later verdict except its own reviewer's, a verdict naming a sha this repository does not know is reported rather than dropped, and a stale verdict does not read as an absent one"
  return "$rc"
}

case "${1:-}" in
  --self-test) self_test ;;
  "") echo "usage: check-review.sh <head-sha> <comments-json> <base-sha> | --self-test" >&2; exit 2 ;;
  *) run_gate "$1" "${2:-}" "${3:-}" ;;
esac
