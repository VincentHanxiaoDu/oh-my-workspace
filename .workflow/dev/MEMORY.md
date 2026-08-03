# What the dev role has learned about this project

**This file is yours, and the installer never overwrites it.** Write to it the moment you learn
something that cost you time and would cost the next round the same time — how this project actually
behaves, where the traps are, and what you tried that did not work.

Not here: work state (that is Issues and pull requests), decisions (those are `[owner-ruling]` on
the Issue), project configuration (that is `.workflow/PROJECT.md`), or how the process works.

Newest first. Date every entry. **Delete what has stopped being true** — a role acting confidently on
a stale note is worse off than one that knew nothing.

## 2026-08-03 — `go test` caches, and it does not invalidate on a changed shell script

`-count=1` on **every** `go test` here, without exception. The `internal/machinery` tests execute the
installed `.workflow/bin/*.sh`, and Go's test cache does not know a shell script changed — so an
edit to a gate, or a whole framework refresh, reports the previous run's result. Two roles have now
each spent a round on this. Also run the **whole package**, never a name-filtered `-run`: on #100 a
test named for the defect passed under a live mutation while the covering assertion lived in a
differently-named test.

## 2026-08-03 — a framework refresh commit must be trailered `Agent: pm`, or the suite goes red

`frameworkpaths_test.go` classifies commits over `.workflow/bin/` and `.github/` by their `Agent:`
trailer: `pm` is a refresh, anything else — including `dev` — is a **local fix**, which then needs a
line in `internal/machinery/framework-local-commits.txt` or the check names the file and fails.
`check-naming.sh` accepts any non-empty trailer, so nothing stops you writing `Agent: dev` on an
install and discovering this from a red build. **Split a refresh into two commits**: the framework
files as `Agent: pm`, your own test and declaration changes as `Agent: dev`.

## 2026-08-03 — a stub `gh` returning `[]` for everything is a broken fixture, not a simple one

The machinery fixtures drive real scripts. `pulls/<n>` and `check-runs` return **objects**; a stub
answering `[]` makes `pr.sh state` print a raw `jq: error … Cannot index array with string "head"`
into the status column, and it rode along invisibly inside green tests for as long as no assertion
read that column. Give every endpoint the shape the real API gives, and assert that no `jq: error`
appears anywhere in the output — a fixture defect otherwise reads as a finding about the code.

## 2026-08-03 — the four watch self-tests were flaky by construction, and are not any more

`watch-queue.sh --self-test` and `watch-prs.sh --self-test` slept a fixed three seconds and asserted
on whatever had arrived. Observed failing once and passing six consecutive times. The #117 refresh
replaces the sleeps with a bounded poll, but `budgetguard_test.go`'s
`TestTheWatchHoldsOnASecondaryRateLimit` still keys on the new 60-second ceiling and was seen taking
60.02s and failing once, then 2.2–2.8s and passing on every later run. **If it is the only red, run
the package again before believing it.**

## 2026-08-03 — `queue.sh` routes on the branch name now, and one accepted prefix routes nowhere

Since #117, whose pull request something is comes from `<role>/…` in the branch name, not from the
`Agent:` trailers. `check-naming.sh` also accepts `flow/`, for which there is no role and no queue,
so such a pull request is in **nobody's** review section and nothing says so — Issue **#126**, open.
Name your branches `dev/<type>/<issue>-<slug>` and they route.
