# Choose who can see a note

## Why

PRD §3.3 says default visibility is company-wide, "because a knowledge system that defaults to
private has no knowledge in it", and that a note can be narrowed to named people, to a group, or to
yourself. Nothing of that existed: there was no hub-side code at all.

Two things make this more than a permissions field.

The first is PRD §2.4. The hub reads everything published to it, including notes narrowed to a
group or to yourself, because it indexes them. A person who narrows a note and is not told this can
believe something is private when it is not. So the statement belongs at the point of choice, every
time, on every surface — not in documentation and not at onboarding.

The second is PRD §4.3. A group whose membership cannot be resolved, a hub that cannot be reached,
a record that cannot be read: none of those is "this note is visible to nobody". Visibility has to
be a three-valued answer or the product will quietly report a negative it never established.

This change is also the hub's first existence in the repository. Issues #11, #13, #14, #15 and #22
build on it, and #15 in particular needs the visibility predicate — PRD §3.5, "visibility is a
precondition of ranking" — as something it can call rather than reimplement.

## What Changes

- **New package `internal/hub`** — the company-side service: notes, versions, the hub's own record
  of people and groups, visibility, the scope vocabulary and grant refusal.
- **`hub.CanRead`** — one pure, exported, three-valued visibility predicate over
  (visibility, author, reader, membership). Everything that decides who may see something calls it.
  Search, statistics, references and archived-people listings will call exactly this.
- **Four visibility states plus a default** — company-wide (what "no choice expressed" means),
  named people, group, self. Each renders distinguishably; so does the undetermined answer.
- **§2.4 stated at every point of choice** — one `RestrictionStatement` constant, a `CheckSurface`
  rule, and tests that grep the real CLI output and the real agent-API schema for
  "private" / "encrypted" / "secret" / "only you can see this" and require the statement alongside.
- **The timeline is not a bypass** — visibility lives on the note, never on a version, so narrowing
  a note takes its history with it.
- **Groups resolve against the hub's own record** — no directory, LDAP or SSO client exists to be
  consulted; evaluation works with no directory integration present.
- **Grants** — one scope vocabulary shared by CLI, agent API and hub, and
  `EvaluateGrantRequest`, which refuses a grant wider than its holder outright rather than issuing
  a narrower one.
- **New command `omw visibility`** — `choices`, `plan`, `show`, `schema`, `scopes`.

Not in this change, because other Issues own them: the local store (#3), the outbox (#9), sign-in
and token material (#19), search ranking (#15), the client-to-hub transport (unassigned).
