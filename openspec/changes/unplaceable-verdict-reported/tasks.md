# Tasks

## 1. First establish the Issue is not already fixed

- [x] 1.1 Drive the #38 shape against `check-review.sh` **with #88 and #93 already applied**, since
      the selector has been rewritten twice. Result: exit 0, `review ok`, the unplaceable sha
      nowhere in the output. Genuinely still open, and no duplicate work.

## 2. Break it and watch it go red

- [x] 2.1 Add `TestAVerdictNamingAnUnknownShaIsReported`, executing the installed script, `-count=1`.
- [x] 2.2 Reproduce the eight-character prefix collision rather than using two obviously different
      shas. The collision is what made the live case dangerous instead of obvious.
- [x] 2.3 Run the Issue's own control inside the fixture: the head must resolve and the ghost must
      not. If either fails, SKIP with a reason saying nothing was determined — not pass.
- [x] 2.4 Confirm the red is the Issue's: exit 0 and a clean `review ok` over a dropped refusal.

## 3. The fix

- [x] 3.1 Scan every well-formed verdict block for shas that are not the head.
- [x] 3.2 Report only those naming no object; leave a known-commit sha silently stale.
- [x] 3.3 Name both the unplaceable sha and the head, and give the remedy.
- [x] 3.4 Add exit code 4 and its own sentence in the publish step of `gates.yml`.
- [x] 3.5 Fix the precedence explicitly: 4 over 0 and 1, under 2. Pin it with a test arm so it is a
      decision and not a consequence of line order.
- [x] 3.6 Handle the early `no review found` return, where the unplaceable fact would otherwise have
      been lost.
- [x] 3.7 Probe for a shallow checkout and say the question could not be determined, rather than
      accusing on an object that was merely never fetched.
- [x] 3.8 Define fence-stripping once and share it between both jq programs, so the selector and the
      scan cannot drift apart about what a quotation is.
- [x] 3.9 Add self-test arms — both controls, the #38 shape, the stale case, the quoted case, the
      precedence case, and the all-zeros case.
- [x] 3.10 Declare both framework-owned files in `framework-local-commits.txt`.

## 4. Both directions, and the mutations

- [x] 4.1 An ordinary stale review stays silent and still passes.
- [x] 4.2 A quoted verdict naming an unknown sha is not reported — otherwise every postmortem that
      pastes a bad sha turns a pull request red.
- [x] 4.3 A clean independent approve is unaffected.
- [x] 4.4 An unplaceable *approve* is reported too: the defect is the silence and it does not know
      what the verdict said.
- [x] 4.5 Four mutations, each confirmed **by diff** and restored, each red: the exit-code
      conversion disabled; the `cat-file` test removed so stale collapses into unplaceable; the
      shallow probe removed; the rc=4 case removed from the publish step.
- [x] 4.6 Update the two fixtures that spelled "another head" as forty zeros, and pin the all-zeros
      case as unplaceable in its own arm.
- [x] 4.7 `check-review.sh --self-test` and `pr-authors.sh --self-test`.
- [x] 4.8 Full `internal/machinery` suite green, including every #65 and #82 test this is stacked on.
- [x] 4.9 `make ci` and `./.workflow/bin/run-gates.sh` green.

## 5. Errors made building this, recorded because they were caught by tests and not by reading

- [x] 5.1 A blanket escape-normalisation corrupted the pre-existing self-test fixtures; the arms then
      ran against stale JSON and the script reported "command not found" while still exiting 0. A
      self-test that passes while its fixture never got written is a false green, and it was visible
      only because the noise was on stderr.
- [x] 5.2 A backtick inside a double-quoted fixture opens command substitution, and inside a
      single-quoted one a backslash-backtick is an invalid JSON escape. Same character, opposite
      escaping, one line apart — both spellings are now present deliberately and commented.

## 6. Not part of this change

Sweeping the board for other verdicts of this shape. The Issue asks for it and notes that by
construction nobody has noticed any of them. It is an audit of the past and it does not change the
gate; it is not carried here as an unticked box.
