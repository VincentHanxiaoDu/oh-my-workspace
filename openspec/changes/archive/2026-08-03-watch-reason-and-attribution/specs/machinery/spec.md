# machinery

## ADDED Requirements

### Requirement: A failed lookup SHALL show the part of its output that says what failed
A watch that reports a failed lookup SHALL show the END of the captured output when that output is
longer than the space available for it, because the captured stream is everything the command
printed before it died and the failure is its last line. Output that fits SHALL be shown whole and
unaltered.

Where output was elided, the rendering SHALL say so, so that an abbreviated reason does not read as
a complete one.

#### Scenario: The command answered at length and then failed
- **WHEN** a watch's lookup prints more normal output than the reason budget allows and then fails
  with an error on its last line
- **THEN** the reported reason contains that error

#### Scenario: The whole output fits
- **WHEN** a watch's lookup fails with an output shorter than the reason budget
- **THEN** the reported reason is that output unchanged, with nothing trimmed and nothing added

### Requirement: A red default branch SHALL name the failing check and SHALL NOT assert an unmeasured cause
A watch reporting that the default branch is red SHALL name the check that is failing. It SHALL NOT
attribute the failure to whoever merged last: the default branch can be reddened by commits nobody
here merged, including direct pushes by the framework, so who merged last is a proxy for authorship
that stops measuring it exactly when it matters.

Where the failure IS attributable to the caller's own merge, derived by comparing the failing commit
with the merge commit that caller produced, the watch MAY say so. Where it cannot be derived, the
watch SHALL say the cause was not determined rather than guess.

A check name that could not be read SHALL NOT be rendered as a red run with nothing failing in it.

#### Scenario: The default branch is red on a commit no merge here produced
- **WHEN** the default branch's failing commit is a direct push rather than the caller's merge
- **THEN** the event names the failing check, says the cause was not determined, and does not tell
  the caller the failure is theirs to fix

#### Scenario: The failing commit is the caller's own merge commit
- **WHEN** the default branch's failing commit is the merge commit the caller produced
- **THEN** the event may say the failure is theirs to fix, on the strength of that comparison

#### Scenario: The failing check cannot be read
- **WHEN** the default branch is red and the query naming which check failed cannot be answered
- **THEN** the event still reports the branch as red and says the failing check could not be
  determined
