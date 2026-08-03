# Tasks

## Reproduce the defect QA found

- [x] Rename `ErrNoModel` to `ErrModelMissing` across all six referencing files on the merged tree,
      leave the guard's name list stale, plant the cross-package bypass — confirm the OLD guard
      passes green with the bypass present

## Stop identifying the values by their spelling

- [x] `orderingRuleNames` discovers the guarded identifiers by reading `internal/model` for values
      declared with the codes `no-model` and `model-provider-extension-failed-to-load`
- [x] No identifier name is written down in the guard
- [x] `TestTheOrderingRuleIsAnchoredToTheProductCodes` fails loudly when a code stops existing,
      rather than silently tracking the codes that remain

## Resolve the reference rather than string-matching it

- [x] The local binding is resolved from the import PATH: default name, alias, dot-import, and
      `_` bound to nothing (QA evasions A and C)
- [x] Every top-level declaration is walked, `GenDecl` as well as `FuncDecl`, so a package-level
      `var noModel = model.ErrNoModel` is a site (QA evasion B)
- [x] A different package's same-named value is not reported

## Prove it, on the real tree and not only in fixtures

- [x] `TestTheGuardSurvivesARenameOfTheRefusalItGuards` — QA's recipe, with the refusal spelled a
      name that appears nowhere in the guard
- [x] `ErrNoModel` renamed repo-wide across all six files on the REAL tree, bypass planted in
      `internal/agentapi`, `go build ./...` clean, guard RED and naming
      `internal/agentapi.plantedReviewDecision`
- [x] CONTROL: the same rename WITHOUT a bypass stays green, so this is not a false positive
- [x] CONTROL: changing a product code fails the anchor test — and the first attempt at that
      mutation matched nothing, so it was reapplied and confirmed by `git diff`
- [x] Tree restored afterwards and `git status` confirmed clean
- [x] Full `internal/commands` package, `-count=1`, no `-run` filter; `make ci`; `run-gates.sh`

**Not closed, and named rather than left to be rediscovered:** a bypass that reaches the value
through a THIRD package — re-exported and then referenced — is not resolved by an AST walk; it needs
`go/types` and a full import graph. The guard's doc comment says so.
