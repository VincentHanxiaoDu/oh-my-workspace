# The one-decision guard survives a rename of the value it guards

## Why

#112's criterion 4 asked for "a guard that fails when a review-mode decision is made without
consulting it — the structural kind, scanning **every** package rather than the one the check lives
in". The guard shipped in #115 does scan every package, is AST-based rather than grep, and carries a
control that fails on an empty result. **It is still defeated by a rename**, and QA found it:

```
theOrderingRule = {"ErrNoModel", "ErrProviderFailedToLoad"}   vs   sel.Sel.Name
```

Renaming `ErrNoModel` to `ErrModelMissing` across the six files that reference it — leaving the
guard's own map stale, which is the ordinary shape of a refactor that forgets a guard — compiles,
and the guard goes **green with the cross-package bypass still in the tree**. The `len(sites) == 0`
control does not save it: the other name still matches, so the list has length one and the guard
reports success while tracking half a rule.

**That is #108's documented failure mode word for word** — "fires on `AttachmentCount`, goes green
when the identical violation is renamed `Attachments`" — and it is the worst kind, because a guard
that reports success manufactures assurance where a missing guard would at least be missing.

QA planted three further evasions, each of which compiles and each of which is ordinary Go:

| | evasion | why it was missed |
| --- | --- | --- |
| A | `import m ".../model"; return m.ErrNoModel` | the base identifier is not the word `model` |
| B | `var noModel = model.ErrNoModel` at package level | the walk inspected only `FuncDecl`s |
| C | `import . ".../model"; return ErrNoModel` | there is no `SelectorExpr` at all |

B is the one to expect by accident: shortening a long reference with a package-level variable is
idiomatic, and nobody doing it is trying to evade anything.

**A second name-matcher with a longer list is the same defect.** The repair has to stop identifying
these values by their spelling.

## What Changes

- **The guarded identifiers are discovered, not written down.** The guard reads `internal/model` and
  asks which values are declared to carry the codes `no-model` and
  `model-provider-extension-failed-to-load`. Those codes are the product's machine-readable
  contract — the entire reason the two refusals are distinct values is that "sharing a code would
  make the two situations indistinguishable to exactly the caller that has no English to inspect".
  A Go rename cannot touch them; changing one is a breaking product change that arrives through the
  front door. **No identifier name appears in the guard.**
- **A code that stops existing fails the build loudly** rather than silently shrinking what the
  guard tracks — the failure mode that let the stale half-list pass.
- **The reference is resolved from the import path**, so an alias, a dot-import and the default
  name are all the same thing to the rule, and **every top-level declaration is walked**, `GenDecl`
  as well as `FuncDecl`. Evasions A, B and C are closed.
- **A different package's same-named value is not blamed** for this rule. A guard that cries wolf is
  one somebody switches off.

## Impact

- `internal/commands/outbox_review_decision_guard_test.go` only. **No production code changes**, so
  #112's criteria 1, 2 and 3 — verified met by product and by QA on #115 — are untouched.
- No new dependency. `go/types` would resolve references properly and is not used: it needs a full
  import graph and a type-check of every package to answer a question the import path already
  answers. The one evasion it would additionally close is named in the guard's own doc comment
  rather than left to be rediscovered — a bypass that reaches the value through a third package.
