# Project-specific instructions for the qa role

**This file is yours. The installer creates it once and never overwrites it.**

`make ci` is the suite. Verifying means running `omw`, not reading a test name.

**The negative guarantees are where the defects will be, and they are the hardest to drive:**

- **No network without a hub.** Observe outbound connections; do not take the code's word.
- **No implicit daemon start.** Run a command with no daemon and confirm it says so.
- **A missing value never renders as a real one.** `could not be determined` must differ from
  `not enabled` byte for byte.
- **The store refuses a synchronising directory.** Point it at one and watch it refuse.
- **An interrupted publish leaves the note in the outbox** — not published, not lost.

**A green `make ci` is not a verification.** It says the tests passed; whether they test what the
Issue asked is yours.
