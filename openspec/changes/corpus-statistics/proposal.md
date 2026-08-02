# Corpus statistics an agent can ground itself on

## Why

PRD §3.5 names two consumers of search and gives them different needs: "People search to find an
answer. Agents search to ground themselves, and need **corpus statistics** — what exists, how much,
how recent — to search well rather than guess."

Issue #15 landed search and excluded statistics in as many words. Without them an agent fires a
query, gets three results, and cannot tell whether that is the whole corpus on the topic or the thin
edge of two hundred notes it phrased badly for. It guesses — and the guess is PRD §1's fourth
diagnosis, *it is in there somewhere and nobody can find it*.

Two properties matter more than the numbers.

The first is that **a statistic is ranking's raw material**, so PRD §3.5's "ranking never surfaces
the existence of something the searcher cannot read" applies to it whole. *How much exists* is
precisely the number that leaks existence: a count of 40 where the reader may read 12 has told them
28 things exist that they are not allowed to know exist. The leak arrives through the statistics
door instead of the results door, and it is the same leak.

The second is PRD §4.3. **A statistic that could not be computed is undetermined, never `0`.** A
zero says "I looked and there is nothing there" and an agent will build a plan on it. An unknown
printed as a zero is a lie that plan rests on. The two must not share a rendering and must not share
an exit code.

## What Changes

- **Statistics are a method on `hub.Corpus` and read nothing wider.** `Corpus` is already
  visibility-settled — `Settle` filters through `Store.ListReadable`, which calls `CanReadNote` —
  its fields are unexported and it has no other constructor. Nothing in the new code takes a
  `*hub.Store`, so counting the raw corpus is not reachable by reordering statements.
- **Three statistics, three independent determinacy rules.** What exists (`Subjects`), how much
  (`Count`), how recent (`Recency`) are separately readable and separately capable of being
  undetermined. Partial determination is a real, reachable state rather than a theoretical one: a
  note whose scope membership cannot be resolved makes the count undetermined while recency stays
  determined, because a note older than the newest one we can see cannot change the answer whichever
  way it falls.
- **Undetermined is a state of a type, and the zero value.** `Count`, `Recency` and `Subjects` each
  carry a `tri.Value` whose zero is `Undetermined`, so a statistic nobody set cannot read as a
  determined zero. `Recency` uses all three of tri's values: an instant, a determined "none", and
  undetermined — which is the whole of criterion 13.
- **Every undetermined statistic carries a reason CODE**, not prose. `no-hub-configured`,
  `daemon-not-running`, `hub-unreachable`, `not-signed-in` and `no-local-store` are five different
  facts and a script tells them apart without parsing a sentence.
- **`ErrNoLocalStore`** — there being nowhere to look is a determined fact about this machine and it
  is still not a count of nothing.
- **A report has a local half and a hub half.** PRD §4.4: with no hub configured the local numbers
  are determined values and only the hub half is undetermined. No connection is attempted, and the
  function that would attempt one is not reached.
- **New command `omw stats`**, and `--json` renders the SAME computed report the text rendering
  prints, so the CLI and the agent API cannot disagree about which statistics are undetermined
  (PRD §4.3, §4.5). Exit 0 when everything was determined, 3 when anything was not.
- **No fourth scope.** Statistics need `read` and nothing else; the vocabulary remains exactly
  `read` / `write` / `publish` (PRD §4.5). The hub operator's ability to read everything published
  to it is §2.4's deployment fact, not a grantable capability.

Not in this change, because other Issues own them: the client-to-hub transport (unassigned, so a
configured hub reports as unreachable), sign-in and token material (#19), and what an archived note
of a departed colleague does to these counts (#22, which decides whether it is in the corpus at
all).
