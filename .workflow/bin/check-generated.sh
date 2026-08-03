#!/usr/bin/env bash
# Gate: files that are generated must not be hand-authored.
#
# `openspec/specs/**` is derived from the changes that have been archived. Editing it directly
# produces a specification that agrees with nobody's code and disagrees with the change history —
# and because it reads like a specification, it is believed.
#
# NOT APPLICABLE when the project has no openspec/specs/ — a project that does not generate
# anything is outside this gate, not failing it. Said explicitly, because a silent pass and an
# inapplicable check look identical from a green tick.
#
# Usage: check-generated.sh <base-sha>
#        check-generated.sh --self-test
set -euo pipefail

case "${1:-}" in
  -*) [ "$1" = "--self-test" ] || {
        echo "::error::unknown option '$1'. This is a typo, not an argument — refusing." >&2; exit 2; } ;;
esac

GENERATED_PATHS='openspec/specs'

hand_edit_gate() {
  local base=$1 touched p present=0

  for p in $GENERATED_PATHS; do
    git cat-file -e "HEAD:$p" 2>/dev/null && present=1
  done
  [ "$present" -eq 1 ] || {
    echo "NOT APPLICABLE: this project generates none of $GENERATED_PATHS, so there is nothing here to protect."
    return 0
  }

  git rev-parse --verify --quiet "$base^{commit}" >/dev/null || {
    echo "::error::base commit '$base' is not in this clone, so the changed files cannot be determined." >&2
    echo "  This is a CHECKOUT problem — the job needs fetch-depth: 0 — and NOT a statement that nothing generated was edited." >&2
    return 1
  }

  touched=""
  for p in $GENERATED_PATHS; do
    touched="$touched$(git diff --name-only "$base"...HEAD -- "$p/**" || true)"$'\n'
  done
  touched=$(printf '%s' "$touched" | grep -v '^$' || true)

  [ -n "$touched" ] || { echo "generated files untouched: $GENERATED_PATHS"; return 0; }

  # AN ARCHIVING PULL REQUEST LEGITIMATELY REGENERATES THEM. That is the one edit that is not
  # hand-authoring, and refusing it would make the correct action impossible.
  if git diff --name-only "$base"...HEAD -- 'openspec/changes/archive/**' | grep -q .; then
    echo "generated files changed alongside an archive — that is regeneration, not hand-authoring:"
    printf '%s\n' "$touched" | sed 's/^/  /'
    return 0
  fi

  # THE `## Purpose` SECTION IS THE ONE PART openspec ASKS A HUMAN TO WRITE. Archiving stamps
  # `TBD - created by archiving change <name>. Update Purpose after archive.` into every new spec,
  # and it is generated from nothing — there is no change to go back and edit. So this gate forbade
  # exactly what the tool instructs, on every new capability, with a remedy that does not exist.
  #
  # An edit confined to that section is therefore allowed. Everything else in the file is still
  # derived and still refused.
  # COMPARED WITH THE PURPOSE SECTION REMOVED FROM BOTH SIDES. The first attempt matched the diff's
  # own lines against `TBD|Purpose`, which of course fails: the NEW purpose text contains neither.
  # Strip the section and compare what is left — if that is identical, only the purpose moved.
  strip_purpose() { awk '/^## Purpose$/{p=1; next} /^## /{p=0} !p'; }
  local only_purpose=1 f
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    if ! diff -q <(git show "$base:$f" 2>/dev/null | strip_purpose) \
                 <(git show "HEAD:$f"  2>/dev/null | strip_purpose) >/dev/null 2>&1; then
      only_purpose=0; break
    fi
  done <<<"$touched"
  if [ "$only_purpose" -eq 1 ]; then
    echo "generated files changed only where openspec asks a person to write — the Purpose section:"
    printf '%s\n' "$touched" | sed 's/^/  /'
    return 0
  fi

  echo "::error::these generated files were edited without archiving a change:" >&2
  printf '%s\n' "$touched" | sed 's/^/  /' >&2
  echo "  $GENERATED_PATHS is derived, never authored. Change the spec inside the OpenSpec change" >&2
  echo "  and let archiving regenerate it — an edit here disagrees with the history that produced it." >&2
  echo "  The one exception is the '## Purpose' line, which archiving stamps as TBD and asks you to write." >&2
  return 1
}

# THE OTHER HALF OF THE SAME RELATIONSHIP. The gate above refuses a spec edited WITHOUT an archive.
# This one refuses a spec REGENERATED without the archive being finished — the change directory left
# standing in `openspec/changes/` after its content has already landed in `openspec/specs/`.
#
# ELEVEN OCCURRENCES IN ONE DAY, every one of them caught by a person happening to look and none by
# a mechanism (Issue #77, carried to #108). Occurrence eleven landed inside the 171-second window in
# which the repair for eight-to-ten was open. All three roles have done it.
#
# HOW "SHIPPED" IS DECIDED, because criterion 3 turns entirely on it and a gate that blocks
# in-flight work is worse than no gate:
#
#   A change has shipped when every `### Requirement:` heading in its delta
#   `openspec/changes/<slug>/specs/<x>/spec.md` is ALREADY PRESENT in `openspec/specs/<x>/spec.md`.
#
# That is not a heuristic about intent; it is the literal post-condition of `openspec archive`.
# Archiving merges the delta's requirements into the capability spec and removes the directory. So a
# tree where the merge has happened and the removal has not is the defect, stated exactly.
#
# WHAT IT DELIBERATELY DOES NOT USE. Ticked tasks: measured on this repository, BOTH the shipped
# change and the in-flight one had every box ticked, so completeness separates nothing — and a box
# is a claim, which the tasks gate already warns must never be ticked to clear a gate. Merge state
# of the owning pull request: a GitHub fact, not a repository one, unavailable to a gate reading a
# diff and unavailable at all during an API outage.
#
# THE FAILURE MODE THIS RULE HAS, stated rather than hidden: it can only see a change whose content
# HAS reached the specification. A change that shipped and whose spec was never regenerated at all
# is indistinguishable, by anything in this repository, from one that has not shipped yet — the
# in-flight fixture `outbox-drafts-and-modes` and the shipped-and-unarchived
# `unplaceable-verdict-reported` were alike on every local signal. Refusing to guess there is the
# point: this gate does not block on what it cannot tell.
#
# AND IT DOES NOT ANSWER 'NO' THERE EITHER. Only the full match is a determination. A partial or
# absent match is reported as UNDETERMINED, in its own words, because those are two different facts
# and the first version of this arm gave them one confident rendering — announcing "its work has not
# landed" about a change that had shipped. Not blocking and answering 'no' are not the same act.
archive_gate() {
  local base=$1 rc=0 regen cap dir slug delta reqs req n hit spec

  git cat-file -e "HEAD:openspec/changes" 2>/dev/null || {
    echo "no openspec/changes/ on this head, so no change can have been left unarchived."
    return 0
  }

  # SCOPED TO THE CAPABILITIES THIS PULL REQUEST REGENERATED. Criterion 1 is about the pull request
  # that does the regenerating; asking the whole tree on every unrelated pull request would make
  # every author responsible for somebody else's omission, which is how a gate gets disabled.
  regen=$(git diff --name-only "$base"...HEAD -- 'openspec/specs/**' \
    | sed -n 's#^openspec/specs/\([^/]*\)/spec\.md$#\1#p' | sort -u)
  [ -n "$regen" ] || {
    echo "archive check: this PR regenerates no capability spec, so no archive is owed by it."
    return 0
  }

  for dir in $(git ls-tree -d --name-only "HEAD:openspec/changes" 2>/dev/null); do
    [ "$dir" != archive ] || continue
    slug=$dir
    for cap in $regen; do
      delta="openspec/changes/$slug/specs/$cap/spec.md"
      spec="openspec/specs/$cap/spec.md"
      git cat-file -e "HEAD:$delta" 2>/dev/null || continue
      git cat-file -e "HEAD:$spec"  2>/dev/null || continue

      # A DELTA WITH NO REQUIREMENT HEADINGS ANSWERS NOTHING. "Every one of zero is present" is
      # vacuously true and would accuse an empty file, so it is reported as undetermined instead.
      reqs=$(git show "HEAD:$delta" | grep '^### Requirement:' || true)
      if [ -z "$reqs" ]; then
        echo "  cannot tell for $slug (capability '$cap'): its delta declares no '### Requirement:'," \
             "so whether its content has landed is UNDETERMINED, not answered as no."
        continue
      fi

      n=0; hit=0
      while IFS= read -r req; do
        [ -n "$req" ] || continue
        n=$((n+1))
        git show "HEAD:$spec" | grep -Fxq "$req" && hit=$((hit+1))
      done <<<"$reqs"

      if [ "$hit" -eq "$n" ]; then
        echo "::error::openspec/changes/$slug/ is still here, and its content has already landed in $spec." >&2
        echo "  All $n of its requirements for capability '$cap' are present in the generated spec." >&2
        echo "  That is the state archiving LEAVES BEHIND when only half of it was done: the spec was" >&2
        echo "  regenerated and the change directory was not removed." >&2
        echo "  Resolve it with:" >&2
        echo "      openspec archive $slug" >&2
        echo "  and commit the move together with this regeneration." >&2
        rc=1
      else
        # A PARTIAL OR ABSENT MATCH IS NOT A DETERMINATION THAT THE WORK HAS NOT LANDED, and saying
        # so was this gate's own first defect. It printed `in flight, not blocked … so its work has
        # not landed` about `unplaceable-verdict-reported` — which HAD shipped, in #98 — in the same
        # sentence shape and under the same label as the genuinely in-flight
        # `outbox-drafts-and-modes`. Two different facts, one rendering, and the confident half was
        # false. PRD §4.3: a state that could not be determined is shown as undetermined, never as a
        # 'no'. That rule does not stop applying because the thing rendering it is a gate.
        #
        # The absent case and the partial case are the SAME KIND OF FACT as the empty delta above,
        # and they get the same word. What the gate can determine is that the content HAS landed;
        # everything short of that it cannot, and it now says which.
        echo "  cannot tell for $slug (capability '$cap'): $hit of $n requirements are in $spec." \
             "Whether this change has shipped is UNDETERMINED from this repository — a partial or" \
             "absent match does not establish that it is still in flight. Not blocked."
      fi
    done
  done

  # THE CLOSING LINE SAYS WHAT WAS DETERMINED AND NOT MORE. "Every directory is gone or still in
  # flight" would re-assert, in the summary, the very claim the arm above refuses to make.
  [ "$rc" -eq 0 ] && echo "archive check: no change directory was found whose content has already landed. Anything reported above as UNDETERMINED was not judged either way."
  return "$rc"
}

run_gate() {
  local base=$1 rc=0

  # THE BASE IS CHECKED ONCE, HERE, so a lookup failure cannot reach either arm and be rendered as a
  # finding about the code. hand_edit_gate re-states it for its own callers; this is the guard for
  # the arm added second.
  git rev-parse --verify --quiet "$base^{commit}" >/dev/null || {
    echo "::error::base commit '$base' is not in this clone, so the changed files cannot be determined." >&2
    echo "  This is a CHECKOUT problem — the job needs fetch-depth: 0 — and NOT a statement that nothing generated was edited." >&2
    return 1
  }

  hand_edit_gate "$base" || rc=1
  # BOTH ARMS ALWAYS RUN. Short-circuiting would hide the second finding behind the first, and the
  # two are independent defects with independent remedies.
  archive_gate "$base" || rc=1
  return "$rc"
}

self_test() {
  local tmp rc=0 out me
  me=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' RETURN

  _mk() { local d=$1; mkdir -p "$d"; git -C "$d" init -q -b main
    git -C "$d" config user.email t@t; git -C "$d" config user.name t
    echo x > "$d/seed"; git -C "$d" add -A; git -C "$d" commit -qm "chore: seed"
    git -C "$d" rev-parse HEAD > "$d/.base"; }
  _run() { ( cd "$1" && bash "$me" "$(cat .base)" ) 2>&1; }

  # 1. NO GENERATED TREE AT ALL MUST PASS AND SAY SO — the arm that stops this blocking a project
  #    it does not apply to.
  _mk "$tmp/none"
  out=$(_run "$tmp/none") || { echo "SELF-TEST FAIL: a project with no generated tree was BLOCKED" >&2; rc=1; }
  case "$out" in *"NOT APPLICABLE"*) : ;; *) echo "SELF-TEST FAIL: an inapplicable check did not say so: $out" >&2; rc=1 ;; esac

  # 2. A HAND EDIT WITH NO ARCHIVE MUST FAIL. This is the check.
  _mk "$tmp/hand"; mkdir -p "$tmp/hand/openspec/specs/store"
  echo "# generated" > "$tmp/hand/openspec/specs/store/spec.md"
  git -C "$tmp/hand" add -A; git -C "$tmp/hand" commit -qm "chore: seed specs"
  git -C "$tmp/hand" rev-parse HEAD > "$tmp/hand/.base"
  echo "# hand written" >> "$tmp/hand/openspec/specs/store/spec.md"
  git -C "$tmp/hand" add -A; git -C "$tmp/hand" commit -qm "docs: edit"
  _run "$tmp/hand" >/dev/null 2>&1 && { echo "SELF-TEST FAIL: a hand edit to a generated file PASSED" >&2; rc=1; }

  # 3. THE SAME EDIT ALONGSIDE AN ARCHIVE MUST PASS — regeneration is the correct action.
  _mk "$tmp/regen"; mkdir -p "$tmp/regen/openspec/specs/store"
  echo "# generated" > "$tmp/regen/openspec/specs/store/spec.md"
  git -C "$tmp/regen" add -A; git -C "$tmp/regen" commit -qm "chore: seed specs"
  git -C "$tmp/regen" rev-parse HEAD > "$tmp/regen/.base"
  echo "# regenerated" >> "$tmp/regen/openspec/specs/store/spec.md"
  mkdir -p "$tmp/regen/openspec/changes/archive/2026-01-01-c1"
  echo "- [x] a" > "$tmp/regen/openspec/changes/archive/2026-01-01-c1/tasks.md"
  git -C "$tmp/regen" add -A; git -C "$tmp/regen" commit -qm "chore: archive c1"
  _run "$tmp/regen" >/dev/null 2>&1 || { echo "SELF-TEST FAIL: regeneration alongside an archive was blocked — the correct action is impossible" >&2; rc=1; }

  # 3b. FILLING IN THE `## Purpose` TBD MUST PASS. openspec stamps it on every archive and tells you
  #     to write it; forbidding that made the gate refuse the tool's own instruction on every new
  #     capability, with a remedy — "change it inside the change" — that does not exist, because
  #     Purpose is generated from nothing.
  _mk "$tmp/purpose"; mkdir -p "$tmp/purpose/openspec/specs/greeting"
  printf '# greeting Specification\n\n## Purpose\nTBD - created by archiving change add-greeting. Update Purpose after archive.\n\n## Requirements\nx\n' \
    > "$tmp/purpose/openspec/specs/greeting/spec.md"
  git -C "$tmp/purpose" add -A; git -C "$tmp/purpose" commit -qm "chore: seed specs"
  git -C "$tmp/purpose" rev-parse HEAD > "$tmp/purpose/.base"
  sed -i.bak 's/TBD - created by archiving change add-greeting. Update Purpose after archive./Greeting a person by name./' "$tmp/purpose/openspec/specs/greeting/spec.md"
  rm -f "$tmp/purpose/openspec/specs/greeting/spec.md.bak"
  git -C "$tmp/purpose" add -A; git -C "$tmp/purpose" commit -qm "docs: fill in the purpose"
  _run "$tmp/purpose" >/dev/null 2>&1 \
    || { echo "SELF-TEST FAIL: filling in the Purpose TBD was refused — the gate forbids what openspec instructs" >&2; rc=1; }
  # AND AN EDIT ELSEWHERE IN THE SAME FILE MUST STILL FAIL, or the exception swallows the rule.
  sed -i.bak 's/^x$/hand-written requirement/' "$tmp/purpose/openspec/specs/greeting/spec.md"
  rm -f "$tmp/purpose/openspec/specs/greeting/spec.md.bak"
  git -C "$tmp/purpose" add -A; git -C "$tmp/purpose" commit -qm "docs: edit a requirement by hand"
  _run "$tmp/purpose" >/dev/null 2>&1 \
    && { echo "SELF-TEST FAIL: a hand edit outside the Purpose section PASSED — the exception swallowed the rule" >&2; rc=1; }

  # 4. UNTOUCHED MUST PASS.
  _mk "$tmp/clean"; mkdir -p "$tmp/clean/openspec/specs/store"
  echo "# generated" > "$tmp/clean/openspec/specs/store/spec.md"
  git -C "$tmp/clean" add -A; git -C "$tmp/clean" commit -qm "chore: seed specs"
  git -C "$tmp/clean" rev-parse HEAD > "$tmp/clean/.base"
  echo y > "$tmp/clean/other"; git -C "$tmp/clean" add -A; git -C "$tmp/clean" commit -qm "feat: elsewhere"
  _run "$tmp/clean" >/dev/null 2>&1 || { echo "SELF-TEST FAIL: a PR touching nothing generated was blocked" >&2; rc=1; }

  # 5. AN UNREACHABLE BASE MUST FAIL AND MUST NOT CLAIM IT LOOKED.
  out=$( cd "$tmp/hand" && bash "$me" deadbeefdeadbeefdeadbeefdeadbeefdeadbeef 2>&1 ) \
    && { echo "SELF-TEST FAIL: an unreachable base PASSED" >&2; rc=1; }
  case "$out" in *"not in this clone"*) : ;; *) echo "SELF-TEST FAIL: an unreachable base gave no explanation" >&2; rc=1 ;; esac
  case "$out" in *"generated files untouched"*) echo "SELF-TEST FAIL: a lookup failure reported as 'untouched'" >&2; rc=1 ;; esac

  # ---- the archive arm (Issue #77, carried to #108) -------------------------------------------
  # A shared fixture builder: a repository with one capability spec and one change directory whose
  # delta declares REQ. Whether the change has "shipped" is then varied by what the spec contains.
  _mkchange() { # _mkchange <dir> <spec-body> ; leaves the change in flight, base at the seed
    local d=$1 body=$2
    _mk "$d"
    mkdir -p "$d/openspec/specs/thing" "$d/openspec/changes/c1/specs/thing"
    printf 'schema: spec-driven\n' > "$d/openspec/config.yaml"
    printf '# thing Specification\n\n## Purpose\nA thing.\n\n%b' "$body" > "$d/openspec/specs/thing/spec.md"
    printf '## ADDED Requirements\n\n### Requirement: The thing SHALL be done\nIt SHALL.\n\n#### Scenario: it runs\n- **WHEN** run\n- **THEN** done\n' \
      > "$d/openspec/changes/c1/specs/thing/spec.md"
    printf -- '- [x] a\n' > "$d/openspec/changes/c1/tasks.md"
    git -C "$d" add -A; git -C "$d" commit -qm "chore: seed a change and a spec"
    git -C "$d" rev-parse HEAD > "$d/.base"
  }

  # 6. THE DEFECT: the spec is regenerated and the change directory is left standing. MUST FAIL, and
  #    must name the directory and the command that resolves it — a red that does not say what to do
  #    is how eleven occurrences were each fixed by a person working it out again.
  _mkchange "$tmp/left" "## Requirements\n"
  printf '# thing Specification\n\n## Purpose\nA thing.\n\n### Requirement: The thing SHALL be done\nIt SHALL.\n\n#### Scenario: it runs\n- **WHEN** run\n- **THEN** done\n' \
    > "$tmp/left/openspec/specs/thing/spec.md"
  git -C "$tmp/left" add -A; git -C "$tmp/left" commit -qm "chore: regenerate the spec"
  out=$(_run "$tmp/left") && { echo "SELF-TEST FAIL: a spec regenerated with its change directory left in place PASSED" >&2; rc=1; }
  case "$out" in *"openspec/changes/c1/"*) : ;; *) echo "SELF-TEST FAIL: the failure did not name the change directory: $out" >&2; rc=1 ;; esac
  case "$out" in *"openspec archive c1"*) : ;; *) echo "SELF-TEST FAIL: the failure did not name the command that resolves it: $out" >&2; rc=1 ;; esac

  # 7. THE CORRECT ACTION MUST PASS, UNCHANGED. A gate that fails everything satisfies arm 6 and
  #    stops all work, so this arm is not optional decoration — it is the other half of the claim.
  _mkchange "$tmp/archived" "## Requirements\n"
  printf '# thing Specification\n\n## Purpose\nA thing.\n\n### Requirement: The thing SHALL be done\nIt SHALL.\n\n#### Scenario: it runs\n- **WHEN** run\n- **THEN** done\n' \
    > "$tmp/archived/openspec/specs/thing/spec.md"
  mkdir -p "$tmp/archived/openspec/changes/archive/2026-01-01-c1"
  git -C "$tmp/archived" mv openspec/changes/c1 openspec/changes/archive/2026-01-01-c1/c1
  git -C "$tmp/archived" add -A; git -C "$tmp/archived" commit -qm "chore: archive c1"
  _run "$tmp/archived" >/dev/null 2>&1 || { echo "SELF-TEST FAIL: a correctly archived change was BLOCKED — the correct action is impossible" >&2; rc=1; }

  # 8. WORK THAT HAS NOT SHIPPED MUST STAY GREEN. This is what makes the gate safe to enable: an
  #    in-flight change beside somebody else's regeneration must not be accused. Get this wrong and
  #    the release critical path is blocked by a gate nobody asked for.
  _mkchange "$tmp/inflight" "## Requirements\n\n### Requirement: Something else entirely\nIt SHALL.\n"
  printf '# thing Specification\n\n## Purpose\nA thing.\n\n## Requirements\n\n### Requirement: Something else entirely\nIt SHALL.\n\n#### Scenario: x\n- **WHEN** x\n- **THEN** y\n' \
    > "$tmp/inflight/openspec/specs/thing/spec.md"
  mkdir -p "$tmp/inflight/openspec/changes/archive/2026-01-01-other"
  printf -- '- [x] a\n' > "$tmp/inflight/openspec/changes/archive/2026-01-01-other/tasks.md"
  git -C "$tmp/inflight" add -A; git -C "$tmp/inflight" commit -qm "chore: archive somebody else's change"
  out=$(_run "$tmp/inflight") || { echo "SELF-TEST FAIL: an in-flight change was accused of needing an archive" >&2; rc=1; }
  # AND IT MUST NOT BE ANSWERED AS A 'NO'. `0 of N` was rendered `so its work has not landed`, which
  # is a determination, and it was FALSE about `unplaceable-verdict-reported` on real `main` — a
  # change that had shipped. Not blocking is right; claiming to know is not.
  case "$out" in *"cannot tell for c1"*) : ;; *) echo "SELF-TEST FAIL: the unshipped case passed for the wrong reason: $out" >&2; rc=1 ;; esac
  case "$out" in *"UNDETERMINED from this repository"*) : ;; *) echo "SELF-TEST FAIL: a 0-of-N result was reported as a determination rather than as undetermined: $out" >&2; rc=1 ;; esac
  case "$out" in *"its work has not landed"*) echo "SELF-TEST FAIL: the confident sentence is back — a 0-of-N result is not a finding that the work has not landed: $out" >&2; rc=1 ;; esac

  # 8b. A PARTIAL MATCH IS THE SAME KIND OF FACT and must render the same way. Driven separately
  #     because `hit == 0` and `0 < hit < n` reach it by different arithmetic, and a branch written
  #     for one of them can be wrong for the other.
  _mkchange "$tmp/partial" "## Requirements\n"
  printf '## ADDED Requirements\n\n### Requirement: One\nIt SHALL.\n\n#### Scenario: a\n- **WHEN** a\n- **THEN** b\n\n### Requirement: Two\nIt SHALL.\n\n#### Scenario: c\n- **WHEN** c\n- **THEN** d\n' \
    > "$tmp/partial/openspec/changes/c1/specs/thing/spec.md"
  printf '# thing Specification\n\n## Purpose\nA thing.\n\n## Requirements\n\n### Requirement: One\nIt SHALL.\n\n#### Scenario: a\n- **WHEN** a\n- **THEN** b\n' \
    > "$tmp/partial/openspec/specs/thing/spec.md"
  mkdir -p "$tmp/partial/openspec/changes/archive/2026-01-01-other"
  printf -- '- [x] a\n' > "$tmp/partial/openspec/changes/archive/2026-01-01-other/tasks.md"
  git -C "$tmp/partial" add -A; git -C "$tmp/partial" commit -qm "chore: half of c1's requirements have landed"
  out=$(_run "$tmp/partial") || { echo "SELF-TEST FAIL: a partial match was BLOCKED" >&2; rc=1; }
  case "$out" in *"1 of 2 requirements"*) : ;; *) echo "SELF-TEST FAIL: a partial match did not report the count it measured: $out" >&2; rc=1 ;; esac
  case "$out" in *"UNDETERMINED from this repository"*) : ;; *) echo "SELF-TEST FAIL: a partial match was not reported as undetermined: $out" >&2; rc=1 ;; esac

  # 9. A DELTA WITH NO REQUIREMENT HEADINGS MUST BE UNDETERMINED, NOT GREEN-BY-VACUITY. "Every one
  #    of zero is present" is true, and an empty delta would otherwise be accused on nothing.
  _mkchange "$tmp/empty" "## Requirements\n"
  printf '## ADDED Requirements\n' > "$tmp/empty/openspec/changes/c1/specs/thing/spec.md"
  printf '# thing Specification\n\n## Purpose\nRewritten.\n\n## Requirements\n' \
    > "$tmp/empty/openspec/specs/thing/spec.md"
  mkdir -p "$tmp/empty/openspec/changes/archive/2026-01-01-other"
  printf -- '- [x] a\n' > "$tmp/empty/openspec/changes/archive/2026-01-01-other/tasks.md"
  git -C "$tmp/empty" add -A; git -C "$tmp/empty" commit -qm "chore: regenerate with an empty delta"
  out=$(_run "$tmp/empty") || { echo "SELF-TEST FAIL: an undeterminable delta was BLOCKED rather than reported as undetermined" >&2; rc=1; }
  case "$out" in *"UNDETERMINED"*) : ;; *) echo "SELF-TEST FAIL: an undeterminable delta was passed silently: $out" >&2; rc=1 ;; esac

  # 10. A PULL REQUEST THAT REGENERATES NOTHING MUST NOT BE MADE RESPONSIBLE for a change directory
  #     somebody else left. Scoping is the whole of criterion 3's safety.
  _mkchange "$tmp/untouched" "## Requirements\n"
  echo z > "$tmp/untouched/elsewhere"
  git -C "$tmp/untouched" add -A; git -C "$tmp/untouched" commit -qm "feat: elsewhere"
  out=$(_run "$tmp/untouched") || { echo "SELF-TEST FAIL: a PR regenerating nothing was blocked by the archive arm" >&2; rc=1; }
  case "$out" in *"regenerates no capability spec"*) : ;; *) echo "SELF-TEST FAIL: the archive arm did not say why it had nothing to judge: $out" >&2; rc=1 ;; esac

  [ "$rc" -eq 0 ] && echo "self-test passed: no generated tree passes and says so, a hand edit fails, regeneration alongside an archive passes, a regeneration that leaves its change directory standing fails naming the archive command, a change directory standing with a partial or absent match is reported UNDETERMINED rather than answered as no, and an unreachable base fails without claiming it looked"
  return "$rc"
}

case "${1:-}" in
  --self-test) self_test ;;
  "") echo "usage: check-generated.sh <base-sha> | --self-test" >&2; exit 2 ;;
  *) run_gate "$1" ;;
esac
