# Tasks

## Find the cause, do not guess at it

- [x] Read `.workflow/bin/pr-authors.sh` and identify every place its answer could turn on git's
      output shape rather than on the repository
- [x] Reconstruct the pre-fix script from history and demonstrate the divergence: `dev` under a git
      whose `show --name-only` output has no leading blank line, `dev, product` under one that does —
      the two answers reported on `dev/feat/7-ticket-merge`
- [x] Confirm the shipped script answers `dev` under both

**Out of scope, and not claimed.** Reproducing the divergence against a REAL git 2.54 was not done:
git 2.50.1 is the only git on this machine, so the blank line was supplied by a stand-in. The
mechanism is demonstrated; the attribution of that blank line to git 2.54 specifically is taken from
the runner's observation in #61 and is inferred, not measured. Tracked as #73.

## The second defect, and whether it is the same one

- [x] Reproduce the archive-only refusal with the exemption working and a single git version, showing
      it is a SECOND root cause and not this one
- [x] Confirm the remedy is a distinguishable second question (`--all-trailers`), not a change to the
      predicate

## A regression test that survives an upgrade

- [x] `internal/machinery/prauthors_test.go`, project-owned, executing the installed script
- [x] A stand-in `git` on `PATH` that perturbs only porcelain, so the output shape is supplied rather
      than borrowed from the git under test
- [x] The predicate driven against literal file lists, covering shapes this machine's git does not emit
- [x] The archive-only pull request asserted end to end through the real `check-review.sh`, and a
      no-trailer branch still refused with the trailer named
- [x] Environment probed by asking — `git --version`, `git rev-parse --is-shallow-repository` — and a
      skip that says it determined nothing rather than a silent pass

## Watch it go red

- [x] Mutate the installed script back to porcelain with no blank-line stripping; verify the mutation
      is present and on the exercised path; `go test -count=1` fails naming `[dev product]` vs `[dev]`
- [x] Mutate `--all-trailers` into a no-op; `go test -count=1` fails on both the trailer assertion and
      the end-to-end `check-review.sh` exit code
- [x] Restore `.workflow/bin/pr-authors.sh` byte for byte and confirm green — nothing is left declared
      in `internal/machinery/framework-local-commits.txt` because no framework file is modified

## Gates

- [x] `make ci` green
- [x] `./.workflow/bin/run-gates.sh` green
