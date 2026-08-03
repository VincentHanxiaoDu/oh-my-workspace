# Choose a model provider and supply your own key

## Why

A person wants their own AI in this product — the `review` publication mode that checks their drafts
against rules they wrote in their own words, and the subscriptions that summarise. That means
choosing a provider and handing over a key. **Their** key: not the company's, not something the hub
hands down, and not something their own AI can read back out and put in a note.

And before they have chosen one, they do not want a half-dead client. Nothing they do today needs a
model — tickets, projects, the outbox, publishing manually. That must keep working exactly as it
does. The moment they ask for something that genuinely needs a model, they want to be told *that* is
what is missing, in those words, not handed an empty result.

## What already existed, and why it had to move

**Issue #9 (PR #38) already read `OMW_MODEL`, `OMW_MODEL_KEY` and `OMW_MODEL_KEY_FILE`**, in
`internal/drafts/model.go`. It had no choice: `review` mode's entire point is what happens when the
model is absent, so #9 could not defer "is a model configured" to an Issue that had not started. Its
author flagged this unprompted and named the invented part — that an unreadable key file was made
the undetermined case because it wanted a genuinely probe-able third value.

**Two implementations of "is a model configured" that disagree is worse than either alone.** It is
the same class of defect as the two outboxes PRD §3.14 forbids, and §4.3's "the control API and the
CLI report the same state" means the answer has to be one answer. So this change does not add a
second reader beside the first; it moves the resolution into `internal/model`, deletes
`internal/drafts/model.go`, and has `internal/commands` consume the one answer. A structural test
now fails the build if any file outside `internal/model/config.go` names those variables.

**The third value is kept, deliberately, and the reasoning is written down.** A credential file that
is named and **does not exist** is a determined negative — the filesystem answered. One that is
named, **exists and cannot be read** is a failure to determine. That is the same line
`tri.FromError` already draws, which is the argument for it: the third value is not a new rule, it
is the project's existing rule applied to a second source. What this change adds is the source #9
could not have — a recorded choice in the store that exists and will not read.

## What changes

- **`internal/model`** is the one answer. It resolves the person's provider and credential from the
  environment and from an explicitly recorded choice, three-valued in both halves.
- **The two halves are two answers.** "Which provider" and "is there a credential" are separate
  facts, because "chosen, no key yet" must be distinguishable from "nothing chosen" and from
  "configured".
- **`omw model`** chooses a provider, names a credential file, reads the configuration back and
  clears the recorded choice. Every one of those is an explicit act; nothing else in the product
  configures a model as a side effect.
- **The product takes no custody of keys.** What is recorded is the provider's name and, optionally,
  the **path** of a file the person owns. The credential value is read from their environment or
  their file at the moment it is used and is never written into the store — so criterion 7's "a full
  export of the local store" has nothing to find.
- **The provider is an interface** (§2.5) with a registry that contacts nothing on registration.
- **The control API carries the model state**, as a type that has nowhere to put a credential, so
  the CLI and the control API can be shown to report the same state.

## What this change deliberately does not do

- **It does not build the extension mechanism.** PRD §2.5 says channel adapters and model providers
  are one mechanism; Issue #21 owns it and has not started. Building the general mechanism here
  would leave #21 doing a migration instead of a design. The interface here is two methods.
- **It ships no provider adapter**, so the registry is empty in this build. Choosing a provider and
  being able to talk to one are separable, and a person who chooses a provider this build has no
  adapter for is told exactly that — a determined fact, distinct from "no provider chosen".
- **It does not implement hub transport.** That is Issue #10. "No hub request carries the
  credential" is driven as far as the build allows and the remainder is stated in the pull request
  rather than faked.
