# A shipped change left in `openspec/changes/` fails a gate

## Why

Eleven occurrences in one day of a change whose spec had been regenerated into
`openspec/specs/<x>/spec.md` while `openspec/changes/<slug>/` was left standing. **Eleven caught by
a person happening to look. Zero caught by a mechanism.** Occurrence eleven landed inside the
171-second window in which the repair for eight-to-ten was open, and all three roles — dev, qa and
product — have done it. Recorded on Issue #77, carried verbatim into #108 when #77 was closed.

Nothing checked it. The relationship between a change and the specification it generates was
enforced in exactly one direction: `check-generated.sh` refused a spec edited *without* an archive,
and said nothing about a spec regenerated with the archive only half done.

## What Changes

- **`check-generated.sh` gains a second arm.** For each capability whose
  `openspec/specs/<x>/spec.md` this pull request regenerates, every unarchived change directory that
  declares a delta for that capability is judged, and one whose content has already landed is
  refused — naming the directory and the `openspec archive <slug>` command that resolves it.
- **"Shipped" is decided on the post-condition of archiving, not on intent.** A change has shipped
  when every `### Requirement:` heading in its delta is already present in the generated capability
  spec. Ticked tasks were considered and rejected: measured on this repository, the shipped change
  and the in-flight one *both* had every box ticked, so completeness separates nothing.
- **The arm is scoped to what the pull request regenerates**, so an author is never made responsible
  for a directory somebody else left standing.
- **A delta declaring no requirements is undetermined, not green.** "Every one of zero is present"
  is vacuously true and would accuse a file that answers nothing.
- **`internal/machinery/archivegate_test.go`** executes the installed script, so a framework refresh
  that removes the arm turns this repository's suite red instead of restoring the defect quietly.

## Non-goals

`unplaceable-verdict-reported`, the live unarchived change on `main`, is **not** caught by this and
the reason is stated rather than hidden: the pull request that shipped it never regenerated
`openspec/specs/machinery/spec.md` at all. On every signal this repository holds — ticked tasks,
implementation on the default branch, delta content absent from the generated spec — it is
byte-for-byte alike with `outbox-drafts-and-modes`, which is legitimately in flight and on the
release critical path. The only fact that separates them is the merge state of their owning pull
requests, which is a GitHub fact and not a repository one. A rule that guessed there would block
#38 and #46, and a gate that stops in-flight work is worse than no gate.
