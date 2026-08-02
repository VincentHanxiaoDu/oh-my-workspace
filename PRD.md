# Oh My Workspace — Product Requirements

**A company's shared memory, and a local client that fills it for you.**

---

## 1. The problem

Companies already know most of what they need. The knowledge is in somebody's head, in a chat thread
from March, or in the reasoning an AI agent produced for one person and nobody else ever saw. Two
people solve the same problem six weeks apart and neither learns the other existed.

Writing things up is the fix, and nobody does it. It is unpaid, it happens after the work is
finished, and the person it helps is a stranger next quarter.

**So the client writes it up.** A person's job shrinks from *write this down* to *let this go*.

### What that buys

When someone's AI hits something it does not know, it searches the hub. If the answer is not there,
the diagnosis is always one of three things:

> Either someone does not know how to search, or I did not write it up clearly enough, or I
> deliberately kept it private — in which case I am the only route to it.

Three causes, all actionable. **The product exists to keep that list at three, and to prevent a
fourth — *it is in there somewhere and nobody can find it* — from ever being the answer.**

---

## 2. Architecture

Two halves. **The boundary between them is the product's central design decision**, not an
implementation detail.

```
┌─────────────────────── ONE PERSON'S MACHINE ───────────────────────┐
│                                                                     │
│   channels ──┐                                                      │
│   (Teams,    │                                                      │
│    email,    ├──▶ ingestion ──▶ ┌─────────┐                        │
│    custom)   │                   │  inbox  │  tickets               │
│              │                   ├─────────┤                        │
│   projects ──┘                   │ outbox  │  draft notes           │
│   (watched dirs)                 └────┬────┘                        │
│                                        │         LOCAL STORE        │
│                                        │      (never leaves except  │
│   ┌──────────┐   control API           │       by publication)      │
│   │  daemon  │◀────────────────┐       │                            │
│   └──────────┘   (local only,  │       │                            │
│        │          owner-only)  │       │                            │
│        │                        │       │                            │
│   ┌────┴─────┐   ┌──────────┐  │       │                            │
│   │   CLI    │   │ agent API│──┘       │                            │
│   │  (omw)   │   │ (your AI)│           │                           │
│   └──────────┘   └──────────┘           │                           │
└──────────────────────────────────────────┼───────────────────────────┘
                                           │ publish
                                           ▼
┌─────────────────────────── THE HUB (one per company) ───────────────┐
│   notes · versions · references · visibility · search · people      │
└──────────────────────────────────────────────────────────────────────┘
```

### 2.1 The client — local, private, yours

A long-running process on each machine a person uses. It connects to their **channels**, watches the
**projects** they point it at, and keeps everything in a local store.

| Component | Responsibility |
|---|---|
| **daemon** | The long-running process. Watches projects, ingests from channels, owns the store's write lock. |
| **local store** | Every ticket and draft. The sole home of unpublished data. |
| **control API** | A local-only interface the CLI and a person's AI both read the daemon through. |
| **CLI (`omw`)** | What a person types. |
| **agent API** | What a person's own AI reads their private material through. |
| **channel adapters** | Teams and email built in; an interface for anything else. |
| **model providers** | Which model serves agent subscriptions. Same extension mechanism as adapters. |

**One store per device, created explicitly. One daemon per store**, enforced by an exclusive lock —
a second cannot start against the same store.

### 2.2 The hub — one per company

Everyone logs in and sees their own material plus whatever colleagues have made visible. Notes are
searchable by person, by group, or company-wide, and carry **references** to people, groups and other
notes — so *what else was written about this* is a question with an answer.

### 2.3 The three containers, which are not the same container

| | Holds | Lives |
|---|---|---|
| **inbox** | tickets — what is being asked of *you* | on your machine, never published |
| **outbox** | draft notes — what you *might* tell others | on your machine, until you publish |
| **hub** | published notes | shared, subject to visibility |

**Nothing leaves the machine unless the person publishes it.**

### 2.4 What the hub can read, stated plainly

Everything published to it, **including notes restricted to a group or to yourself.** It has to — it
indexes them so they can be found.

**Restriction controls which *colleagues* see a note. It is not a wall against whoever operates the
server.** The genuinely private thing is the thing never published, which is the third branch of the
loop in §1.

### 2.5 One extension mechanism, two interfaces

Channel adapters and model providers are the same mechanism with two interfaces. A company adding a
channel and a company choosing a model do the same kind of thing, and should not learn two systems.

---

## 3. Functional design

### 3.1 Channels and ingestion

The client connects to the channels a person is contacted through. **Teams and email ship built in**;
the **channel adapter interface** is the extension point for anything else.

Ingestion is continuous while the daemon runs. It produces tickets — not a mirror of the traffic.

### 3.2 Tickets

**A ticket is a thing you have to act on. It is not a message.**

Five emails, a chat thread and a follow-up ping about one broken login are **one ticket**, with a
written title and summary — not five items titled `yes`, `ok` and `Hii`.

- **Merging crosses channels**, because a problem does not respect the boundary between a mailbox and
  a chat client.
- **Every merge is reversible and shows its working**: what was merged, from where, and why.
- **Acknowledgements and small talk are not low-priority tickets. They are not tickets.**

Tickets live in the inbox and are never published.

### 3.3 Notes and the outbox

A note is a unit of published knowledge — something a person or their AI worked out and is willing to
let others find.

**Three publication modes, and the choice is the person's:**

| Mode | Behaviour |
|---|---|
| **`manual`** *(default)* | Drafts accumulate in the outbox and go nowhere until the person says so |
| **`review`** | An AI checks each draft against rules the person wrote **in their own words** |
| **`auto`** | Drafts publish directly |

**Default visibility is company-wide**, because a knowledge system that defaults to private has no
knowledge in it. A note can be narrowed to named people, to a group, or to yourself.

**Notes are versioned.** Search finds the latest; the timeline is addressable, so a claim someone
acted on last month can still be read as it stood.

**Notes outlive employment.** A deactivated person's notes are archived, not deleted — the knowledge
was the point.

### 3.4 References

Notes carry inline references to **people, groups and other notes**. This is what makes the corpus a
graph rather than a pile, and what makes *what else was written about this* answerable.

### 3.5 Search

Scoped to **person, group, or company**. Two consumers with different needs:

- **People** search to find an answer.
- **Agents** search to ground themselves, and need **corpus statistics** — what exists, how much, how
  recent — to search well rather than guess.

**Visibility is a precondition of ranking.** What a searcher may see is settled before how results are
ordered; ranking never surfaces the existence of something the searcher cannot read.

### 3.6 Projects

A person points the client at directories that matter. The client watches them.

**Watching is a poll, not an instant notification.** While the daemon runs it re-examines each watched
directory every couple of seconds, so a change is reflected within about that long.

**With no daemon running, nothing watches at all** — a project listing looks at each directory during
that command instead, **and says which of the two happened.**

**A missing project directory is marked, never dropped.**

### 3.7 Reports and subscriptions

Separately from the knowledge loop, the client reports on a person's own work.

A **subscription** is a standing instruction built from **selectors**, each naming a **subject** and a
**granularity**:

```
git:full              every commit, with its message
token_usage:digest    model spend, rolled up
*:summary             one paragraph per subject, for everything
git.commit:event      commits without their text
*, !channel           everything except channel traffic
```

**Five granularities, ordered by detail** — `full`, `event`, `digest`, `summary`, `count`. They mean
the same thing for every subject, which is what makes `*:summary` a sensible thing to ask for.

### 3.8 Devices

Each machine is registered under **a label unique to the person**. Devices are separate and are shown
as separate. **Every device is listed, including one never started** — a device that has not checked
in is a fact worth seeing, not an absence.

### 3.9 Status and diagnostics

- **Status** — one screen that says whether everything runs.
- **Diagnostics** — a bundle a person can hand to whoever supports them. **The bundle states what it
  contains**, and **withholds identifying data by default** — raw message bodies are not in it unless
  asked for.
- **Health** — reports the deployment assumptions, including §4.1.

---

### 3.10 Signing in, and what a token can do

A person signs in to the hub. **Their client authenticates as them**, and so does anything they
delegate to — their own AI, a script, another tool.

- **A token carries a scope, not an identity.** "Read my own notes" and "publish as me" are
  different grants, and a token that can do the second was asked for on purpose.
- **A token is revocable and its use is visible.** A person can see what has been signed in as them
  and end any of it.
- **Nothing signs in silently.** A first sign-in is an act the person performs, not one a client
  performs for them.

### 3.11 How a note reaches the hub

Publication is a transfer, and it can fail. **A note that did not arrive is still in the outbox** —
never both, never neither.

- **Interrupted means not published.** A person retries and does not get two copies.
- **The client says which state a note is in** — drafted, in flight, published, or refused — and
  refused says why.
- **A hub that cannot be reached is not a rejected note.** Those are different facts and they are
  reported differently.

### 3.12 A person's own AI, reading their own material

The agent API is how someone's AI sees what only they can see: their tickets, their drafts, and the
hub as they are permitted to read it.

- **It is scoped to that person and nothing wider.** The same authority model as everything else
  (§4.5) — an agent cannot read what its person cannot.
- **It is local.** It reaches the daemon over the control API (§4.6), not over the network.
- **What the AI reads, it can be told to write up** — that is the loop in §1, and this is the half
  that reads.

### 3.13 Which model, and whose key

A person chooses the model that serves their agent subscriptions and their `review` publication
mode, and supplies their own credentials for it.

- **The provider is an interface**, the same extension mechanism as a channel adapter (§2.5).
- **A key belongs to the person who supplied it.** It is not published, not synchronised, and not
  readable through the agent API.
- **No model configured is not a broken client.** Everything that does not need one keeps working,
  and everything that does says what is missing.

### 3.14 The local store

One store per device, created by an explicit act, holding every ticket and every unpublished draft.

- **It survives an interrupted write.** A crash mid-write leaves the store readable, not truncated.
- **It refuses a synchronising location** (§4.1) — the disk is the boundary only while the file
  stays on it.
- **It is the sole home of unpublished data.** Nothing else on the machine is a second copy.

## 4. Product principles

These are decisions, not preferences. They constrain the design.

### 4.1 The disk is the boundary

**The client does not encrypt its own store. It assumes full-disk encryption is enabled** —
FileVault, BitLocker, LUKS. Everything about data staying on a person's machine rests on the disk
being the boundary: anyone who can read an unencrypted disk can read unpublished tickets and drafts.

Two things follow, and a person can see both in the product:

- **Health reporting answers in three values** — `enabled`, `not enabled`, or **`could not be
  determined on this platform`**. It is a report, never a blocker, and it needs neither a store nor a
  running daemon.
- **The store refuses to live anywhere that synchronises off the machine** — Dropbox, iCloud Drive,
  OneDrive, a roaming profile — because the disk stops being the boundary the moment a file is copied
  off it.

### 4.2 Nothing implicit

- **No command starts the daemon on a person's behalf.** If it is not running, commands say so.
- **No network connection without a hub configured.** With no hub, every local capability works and
  nothing reaches out.
- **The store is created explicitly**, by a command a person runs on purpose.

### 4.3 The product says what it knows and what it does not

- **The daemon reports its own state**, including how its last run ended.
- **The daemon stops when it cannot write** rather than continuing in a state a person would read as
  healthy.
- **The control API and the CLI report the same state.**
- **A state that could not be determined is shown as undetermined**, never as a "no". A missing
  project directory, a device that has never checked in, a disk whose encryption cannot be read: each
  is a distinct answer, and none of them is silence.

### 4.4 The local half stands alone

Every local capability works with no hub configured. **The client is useful to one person before it
is useful to a company.**

### 4.5 One authority model

**One scope vocabulary across the CLI, the agent API and the hub.** A person reasoning about who can
see what learns one system, and a permission means the same thing wherever it is written.

- **A scope names a capability, not a surface.** "Publish a note" is one grant whether it is
  exercised by a person typing, by their AI, or by a script holding a token.
- **Nothing is implicitly wider than what was asked for.** A grant that would let something read
  more than its holder can is not narrowed at the edge; it is refused when it is requested.
- **The hub is not exempt.** Whoever operates it can read what is published to it (§2.4) — that is
  stated because it is true, and no scope pretends otherwise.

### 4.6 The control API is local, and demonstrably so

**Not reachable from another machine.** It confirms its socket is owner-only before opening, and
**does not open if it cannot confirm it.**

---

## 5. Open product questions

1. **Platforms.** Which operating systems is this product for? And where §4.6's local-only guarantee
   cannot be confirmed on a platform, what should a person get — a client that runs without a control
   API, or one that declines to run there at all?
2. **Hub-side AI.** §3.3's `review` mode runs on the client. Is there a hub-side counterpart — a
   company-wide rule set applied at publication — or is review purely personal?
3. **Groups.** §3.3 narrows visibility to a group and §3.5 scopes search to one. Are groups mirrored
   from the company directory, or defined inside the product?
4. **Retention.** Notes are versioned forever (§3.3) and tickets are never published (§2.3). Does
   anything expire?
