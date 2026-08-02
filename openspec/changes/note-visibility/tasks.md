# Tasks

## The hub package

- [x] Create `internal/hub` with a package doc that says what it is, what it deliberately excludes, and which Issues build on it
- [x] `Visibility` value type with four states and a zero value that is NOT an audience
- [x] `Default()` — the one place "no choice expressed" becomes company-wide
- [x] `Describe()` renderings for the four states, plus `UndeterminedDescription`
- [x] `ParseChoice` — one parser shared by the CLI and the agent API
- [x] `Membership` interface and `Record`, the hub's own group membership record
- [x] `CanRead` — the three-valued visibility predicate, as a free function over values
- [x] `CanReadNote` — the note-level predicate that governs every version
- [x] `Store` with `Publish`, `Amend`, `SetVisibility`, `Read`, `ReadVersion`, `VisibilityOf`, `ListReadable`
- [x] Distinguishable error values, each carrying a stable machine-readable code

## §2.4 at the point of choice

- [x] `RestrictionStatement` constant, saying what restriction controls, what it does not, and why
- [x] `CheckSurface` — the rule that a point of choice, or any overclaiming word, requires the statement
- [x] `ChoiceBlock` — the shared point-of-choice text every CLI surface prints
- [x] Agent API schema built in code, with the statement on the visibility field's own description

## Grants

- [x] One scope vocabulary, shared by CLI, agent API and hub
- [x] `EvaluateGrantRequest` — a grant wider than its holder is refused, never narrowed
- [x] `Ledger` so that "no token was issued" is assertable
- [x] `ReadThrough` — an agent reads exactly what its person can

## The CLI

- [x] `omw visibility choices` — the four choices and the §2.4 statement
- [x] `omw visibility plan <choice>` — works fully with no hub configured
- [x] `omw visibility show <note> [--as p]` — real value, refusal, and undetermined, with distinct exit codes
- [x] `omw visibility schema` — the agent API schema, byte for byte the hub's
- [x] `omw visibility scopes` — the hub's vocabulary, not a copy

## Tests

- [x] Default reads back as `company`, never empty and never "unset"
- [x] The four states and the undetermined rendering are PAIRWISE distinct, in the hub and at the CLI
- [x] A colleague outside a narrowing cannot read the note and does not appear to have it listed
- [x] A member of every group still cannot read a self-only note
- [x] Narrowing excludes a reader from the note's earlier versions through the timeline
- [x] `Version` carries no visibility field (structural, so #11 cannot reintroduce the bypass)
- [x] Group narrowing resolves against the hub's record only, and follows joins and leaves
- [x] An unknown group is refused at publication and the store is unchanged
- [x] "Refused" and "no such note" are distinguishable, in the hub and at the CLI
- [x] An unresolvable group is undetermined, never a refusal, and is not dropped from a listing
- [x] A grant wider than its holder is refused and the person's grant set is unchanged
- [x] The CLI's scope list is exactly the hub's vocabulary
- [x] Grep the real CLI output and the real schema for "private"/"encrypted"/"secret"/"only you can see this" and require the §2.4 statement
- [x] The statement survives a hundred invocations — not an onboarding step
- [x] With no hub configured, `plan` works fully and `show` says exactly what is missing
- [x] With no hub configured, no visibility surface can reach `net` (import-graph test, toolchain probed not assumed)
- [x] The daemon probe reads the socket it is told about and never starts anything

## Verification

- [x] `make ci` green
- [x] Mutate the code five ways and watch the tests go red, then revert (mutations and messages recorded in the PR body)
