# Tasks

## Drive the defect first

- [x] A test driving `omw outbox draft` with a broken extension and no credential, red against the
      old gate on all four assertions — code, reason carried, `no-model` absent, persisted state
- [x] A test driving the same command with a WORKING extension and no credential, which passed
      before the change and must still pass after it

## One decision

- [x] `outboxReviewModelRefusal` — all three situations, the exit code, the headline and the
      persisted state reason, with the ordering left to `model.Readiness`
- [x] `outboxReviewGate` calls it and holds no branch of its own
- [x] `omw outbox draft`'s review-mode case calls it and holds no branch of its own
- [x] `outboxExtensionRefusal` removed rather than left beside its replacement
- [x] No behaviour change where a credential is recorded: `Configured() == tri.Yes` returns
      "not refused" and still reaches `outboxReviewer`

## The guard that counts the sites

- [x] `TestTheReviewModeModelRefusalIsMadeInExactlyOnePlace` — parses every non-test file under
      `internal/`, outside `internal/model`, and permits one function to reference `ErrNoModel` or
      `ErrProviderFailedToLoad`
- [x] It fails on an empty result rather than passing, so a broken walk cannot read as compliance
- [x] `TestTheReviewModeDecisionGuardFiresOnAPlantedBypass` — a bypass planted in a different
      package is named, with the decider itself as the control
- [x] Comments and string literals are not counted as statements of the rule
- [x] The guard driven against the REAL repository with a compiling bypass in `internal/agentapi`,
      and confirmed red

## Verification

- [x] One mutation inside the decision function reddens both the draft test and the review test,
      proving both gates consult it
- [x] Full `internal/commands` package, `-count=1`, no `-run` filter
- [x] `make ci` and `./.workflow/bin/run-gates.sh`
