# Tasks

## The one answer, and the reconciliation with Issue #9

- [x] Add `internal/model` as the single resolution of "which provider is chosen, and is a
      credential supplied" — the environment and an explicitly recorded choice, three-valued in
      both halves
- [x] Delete `internal/drafts/model.go`, which read `OMW_MODEL`, `OMW_MODEL_KEY` and
      `OMW_MODEL_KEY_FILE` because Issue #18 had not landed when Issue #9 needed the answer
- [x] Point `internal/commands/outbox_cmd.go`'s three call sites at `model.Read`, so `internal/drafts`
      no longer reads the environment at all and there is one reader rather than two
- [x] Keep Issue #9's third value — an unreadable credential file is undetermined, a missing one is a
      determined negative — and write the reasoning into the package comment so the choice is met
      rather than inherited
- [x] Add the third value's second source, which #9 could not have: a recorded choice present in the
      store that will not read
- [x] Move #9's model tests out of `internal/drafts` rather than duplicating them, and leave a note in
      `modes_test.go` saying where they went and what now drives them
- [x] Add `TestExactlyOneFileReadsTheModelEnvironment`, which walks the tree's syntax and fails the
      build if any other file names those variables

## Choosing a provider and supplying a key

- [x] Add `omw model` as a new file in `internal/commands`, touching nothing that already exists
- [x] `omw model use <provider>` records the choice; `omw model key file <path>` records the path of a
      file the person owns; `omw model clear` forgets the recorded choice and says what it did not
      touch; `omw model providers` lists what this build can talk to, and says so when that is none
- [x] Record the provider name and the credential file's PATH, never the credential value, so that a
      full export of the local store has nothing to find
- [x] Refuse `omw model key <value>` deliberately, and say why: a credential typed as an argument
      lands in the shell history
- [x] Report the daemon's state on every model subcommand through `internal/commands/liveness.go`'s
      `daemonLiveness`, start it from none of them, and let none of them require it

## The three negatives

- [x] Keep the credential in an unexported field and define `String` and `GoString`, so that neither
      `%v` nor `%#v` can produce it
- [x] Carry a separate `model.View` across every API boundary — a type with no field a credential
      could occupy — rather than a redacted configuration
- [x] Refuse a request for the credential value with its own machine-readable code, identically
      whether or not a credential exists, so the refusal is not an oracle
- [x] Drive the sentinel sweep over every command in the registry, both output streams, and every
      byte under the store root

## The provider interface

- [x] Add a two-method `Provider` interface and a registry that stores a value in a map and contacts
      nothing, and say in the package comment what Issue #21 will need to unify
- [x] Ship no provider adapter, and report "this build has no adapter for that provider" as a
      determined fact distinct from no-provider-configured

## One state, two surfaces

- [x] Carry the model state on the daemon's report so the control interface and the CLI can be shown
      to agree, and render both from one function
- [x] Collapse the two renderings into one after the agreement test caught them drifting — the
      configuration's rendering now delegates to the view's
- [x] Move "this build has no adapter" into the shared state after the same test caught one surface
      adding lines the other did not

## Driving it

- [x] Drive criterion 11's three states — a passing review, a model that rejected the credential, and
      no model configured — and assert three distinguishable outputs and no shared exit status
- [x] Drive `review` with no hub configured at all, to both a pass and a refusal
- [x] Drive criterion 18 by spawning the real binary twice with one identical environment, with
      `XDG_DATA_HOME` and `HOME` both sandboxed
- [x] Drive the credential across the real control socket and search the bytes that came back
- [x] Probe the undetermined cases on a real filesystem, and skip rather than pass where the
      environment can read a mode-0 file
- [x] Mutate each guarantee, confirm red naming the defect, and revert — recorded in the pull request
