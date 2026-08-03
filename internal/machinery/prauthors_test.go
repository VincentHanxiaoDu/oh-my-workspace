package machinery

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// WHO BUILT A PULL REQUEST MUST NOT DEPEND ON WHICH GIT ASKED THE QUESTION (Issue #61).
//
// `.workflow/bin/pr-authors.sh` is the one derivation of authorship: the routing calls it to decide
// who is independent enough to review, and the review gate calls it to accept or refuse the verdict
// that reviewer posts. When the two disagree, a reviewer is cleared locally, does the work, and has
// the gate refuse the verdict it has just posted — which is the exact failure the script exists to
// end, arriving through the script itself.
//
// It disagreed. `git show --name-only --format=""` is PORCELAIN — output formatted for people, and
// not a stable interface. A blank line in that output does not match `^openspec/`, so the "changed
// nothing outside openspec/" predicate says an archive commit DID touch something else, and its
// author becomes an author. Driven over a fixture, the pre-fix script answers:
//
//	no leading blank line   ->  dev
//	leading blank line      ->  dev, product
//
// THAT BLANK LINE IS NOT GIT 2.54's, AND THIS FILE USED TO SAY IT WAS (Issue #73). The attribution
// was inferred, never measured — only git 2.50.1 was installed on the machine that did the work, so
// the blank line above was supplied by a stand-in `git` on PATH. #73 built a real git 2.54.0 from
// source and asked it, over this same fixture and over the real range that diverged
// (b699d57c..66ddcbaf, pull request #45). It emits NO blank line — byte-for-byte what 2.50.1 emits —
// and the pre-fix script answers `dev` under it, not `dev, product`. Real git 2.55.0 likewise. So
// **no released git is known to emit the shape this test supplies**, and #61's own final root cause
// says the same thing from the other direction: the gate and the tool were different CODE, and the
// git version was never load-bearing.
//
// THE TEST BELOW IS STILL WORTH ITS KEEP, for the reason it was worth keeping before anyone blamed a
// version: reading porcelain to decide authorship is fragile whether or not a shipped git exercises
// the fragility today. It asserts the answer does not move when the porcelain does. It is hardening
// against a latent defect, NOT a regression test for an observed one — do not read a failure here as
// "the runner's git changed".
//
// THESE TESTS DRIVE THE INSTALLED SCRIPT. They never restate its logic. `.workflow/bin/` is
// framework-owned and is replaced wholesale by the next `install.sh` run (Issue #58), so an
// assertion living beside the fix would be replaced along with it; this file is project-owned and
// runs under `make ci`, so a refresh that reintroduces either defect turns this repository red
// instead of quietly restoring it.
//
// AND THE SHAPE OF GIT'S OUTPUT IS SUPPLIED, NOT BORROWED. Every fixture in the script's own
// self-test is built by the same git whose output shape is the variable, so it agrees with itself on
// every machine — which is precisely why the outage was invisible for as long as it was. The test
// below puts a stand-in `git` ahead of the real one on PATH, one that perturbs the porcelain command
// and leaves the plumbing command untouched, and demands the same answer from both. A script that
// reads plumbing, or that strips blank lines, passes; the shipped one does both.
//
// WHAT THIS ESTABLISHES AND WHAT IT DOES NOT. It establishes that a leading blank line in
// `git show --name-only` output flips the answer of the pre-fix script and not of this one — that
// the shipped script is insensitive to the shape of the porcelain. It does NOT establish that any
// real git emits such a line; measured against git 2.50.1, 2.54.0 and 2.55.0, none does (#73). The
// perturbation is chosen to be a shape porcelain COULD take, not one anybody has observed.

// gitProbe is what this file could learn about the git it has to work with. THE POINT IS TO NAME
// WHAT COULD NOT BE ANSWERED rather than to assume a value for it: a test that guesses at its
// environment is the same class of mistake as a script that guesses at git's output shape.
type gitProbe struct {
	path     string
	version  string // the raw `git --version` line
	shallow  string // "true", "false", or "" when the question could not be answered
	unknowns []string
}

// probeGit asks git the two questions this file depends on, and reports which of them it could not
// get an answer to. `--is-shallow-repository` matters because CI checks out at depth 1 and a check
// that assumes history is present is exactly the bug class Issue #61 is about; it is asked of THIS
// repository, and answered rather than presumed.
func probeGit(t *testing.T) gitProbe {
	t.Helper()
	p := gitProbe{}

	path, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("SKIPPING, NOT PASSING: git is not on PATH, so nothing about " +
			".workflow/bin/pr-authors.sh could be determined here.")
	}
	p.path = path

	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		p.unknowns = append(p.unknowns, fmt.Sprintf("`git --version` failed: %v", err))
	} else {
		p.version = strings.TrimSpace(string(out))
	}

	cmd := exec.Command(path, "rev-parse", "--is-shallow-repository")
	cmd.Dir = repoRoot(t)
	out, err = cmd.CombinedOutput()
	switch {
	case err != nil:
		p.unknowns = append(p.unknowns,
			fmt.Sprintf("`git rev-parse --is-shallow-repository` failed: %v: %s", err, strings.TrimSpace(string(out))))
	default:
		switch answer := strings.TrimSpace(string(out)); answer {
		case "true", "false":
			p.shallow = answer
		default:
			p.unknowns = append(p.unknowns,
				fmt.Sprintf("`git rev-parse --is-shallow-repository` answered %q, which is neither true nor false", answer))
		}
	}

	if len(p.unknowns) > 0 {
		t.Skipf("SKIPPING, NOT PASSING: this environment could not be determined, so nothing below "+
			"was checked and no claim is made about .workflow/bin/pr-authors.sh. Unanswered: %s",
			strings.Join(p.unknowns, "; "))
	}
	if p.version == "" {
		t.Skipf("SKIPPING, NOT PASSING: `git --version` printed nothing, so the git under test is unknown.")
	}

	// A SHALLOW CHECKOUT IS REPORTED, NOT ASSUMED AWAY — and it is not a reason to skip here,
	// because every fixture below is a fresh repository this test creates and commits into itself.
	// It needs git to have real history; it does not need THIS repository's history. Anything added
	// to this file that walks the project's own log must skip when this says "true".
	t.Logf("git: %s; this repository is shallow: %s", p.version, p.shallow)
	if p.shallow == "true" {
		t.Logf("this repository is a shallow checkout; the fixtures below carry their own history, " +
			"so that does not affect what is being asserted")
	}
	return p
}

func prAuthorsScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".workflow", "bin", "pr-authors.sh")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s is missing: %v. The routing and the review gate both derive authorship from "+
			"this script; without it there is no single answer to who built a pull request.", path, err)
	}
	return path
}

// shimmedPATH writes a stand-in `git` that perturbs the porcelain output shape — a LEADING AND
// TRAILING BLANK LINE around `git show --name-only` — and delegates everything else,
// including the plumbing `diff-tree`, untouched to the real git. It returns a PATH with the shim
// first.
//
// The blank lines are the whole of the perturbation. Any script that answers differently under this
// PATH is deriving authorship from output formatted for people, and is only as stable as that
// formatting. No released git is known to emit this shape (#73); it is a shape porcelain is free to
// take, which is the whole reason authorship must not be read from it.
func shimmedPATH(t *testing.T, realGit string) string {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"show=0; nameonly=0\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in show) show=1 ;; --name-only) nameonly=1 ;; esac\n" +
		"done\n" +
		"if [ \"$show\" = 1 ] && [ \"$nameonly\" = 1 ]; then\n" +
		"  printf '\\n'\n" +
		"  " + shellQuote(realGit) + " \"$@\"; rc=$?\n" +
		"  printf '\\n'\n" +
		"  exit $rc\n" +
		"fi\n" +
		"exec " + shellQuote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatalf("cannot write the git stand-in: %v", err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// authorFixture is a throwaway repository shaped like a pull request, built with the REAL git so the
// history under test is never itself produced by the stand-in.
type authorFixture struct {
	dir, base, head string
	git             string
	t               *testing.T
}

func newAuthorFixture(t *testing.T, gitPath string) *authorFixture {
	t.Helper()
	needTool(t, "bash")
	f := &authorFixture{dir: t.TempDir(), git: gitPath, t: t}
	f.run("init", "-q", "-b", "main")
	f.run("config", "user.email", "t@t")
	f.run("config", "user.name", "t")
	f.write("f", "seed\n")
	f.run("add", "-A")
	f.run("commit", "-qm", "chore: seed")
	f.base = f.run("rev-parse", "HEAD")
	f.head = f.base
	return f
}

func (f *authorFixture) run(args ...string) string {
	f.t.Helper()
	cmd := exec.Command(f.git, args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *authorFixture) write(rel, body string) {
	f.t.Helper()
	full := filepath.Join(f.dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// commit adds a commit touching the given paths, carrying `Agent: <role>` when role is non-empty.
func (f *authorFixture) commit(subject, role string, paths ...string) string {
	f.t.Helper()
	for i, p := range paths {
		f.write(p, subject+" "+strconv.Itoa(i)+"\n")
	}
	f.run("add", "-A")
	msg := subject
	if role != "" {
		msg += "\n\nAgent: " + role
	}
	f.run("commit", "-qm", msg)
	f.head = f.run("rev-parse", "HEAD")
	return f.head
}

// authors runs the INSTALLED pr-authors.sh over the fixture's range and returns the sorted answer.
func (f *authorFixture) authors(t *testing.T, path string, extraArgs []string) []string {
	t.Helper()
	args := append([]string{prAuthorsScript(t), "--range", f.base, f.head}, extraArgs...)
	cmd := exec.Command("bash", args...)
	cmd.Dir = f.dir
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if path != "" {
		env = append(env, "PATH="+path)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pr-authors.sh --range %s %s %v failed: %v\n%s",
			f.base[:7], f.head[:7], extraArgs, err, out)
	}
	var got []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			got = append(got, l)
		}
	}
	sort.Strings(got)
	return got
}

// TestAuthorshipIsTheSameAnswerUnderEitherGitOutputShape is Issue #61 itself. The archive commit
// touches nothing outside `openspec/`, so it confers no authorship — and that must be true whichever
// git is asked.
func TestAuthorshipIsTheSameAnswerUnderEitherGitOutputShape(t *testing.T) {
	p := probeGit(t)

	f := newAuthorFixture(t, p.path)
	f.commit("feat(x): the work", "dev", "internal/a.go")
	f.commit("chore(x): archive the change", "product", "openspec/specs/x/spec.md")

	plain := f.authors(t, "", nil)
	shimmed := f.authors(t, shimmedPATH(t, p.path), nil)

	want := []string{"dev"}
	for _, c := range []struct {
		name string
		got  []string
	}{
		{"the git on this machine", plain},
		{"a git whose `show --name-only` output carries blank lines — a shape no released git is known to emit, and which porcelain is nonetheless free to take", shimmed},
	} {
		if strings.Join(c.got, ",") != strings.Join(want, ",") {
			t.Errorf("under %s, pr-authors.sh answered %v, want %v.\n"+
				"The archive commit changed nothing outside openspec/, so it confers no "+
				"authorship. If `product` appears here, the derivation is reading git's "+
				"PORCELAIN output — `git show --name-only --format=\"\"`, which is formatted for "+
				"people and guarantees nothing about blank lines — instead of plumbing, or is "+
				"not stripping blank lines before "+
				"testing them against ^openspec/. Read from `git diff-tree --no-commit-id "+
				"--name-only -r`, and drop blank lines. This is Issue #61: the routing cleared a "+
				"reviewer, the reviewer reviewed, and the gate refused the verdict.",
				c.name, c.got, want)
		}
	}
	if strings.Join(plain, ",") != strings.Join(shimmed, ",") {
		t.Errorf("pr-authors.sh gives %v on this machine and %v under a git that formats its output "+
			"differently. ONE DERIVATION MUST BE ONE ANSWER: the routing runs it in a local clone "+
			"and the review gate runs it on the runner, and when those two disagree a reviewer is "+
			"cleared to review work the gate will then refuse it for.", plain, shimmed)
	}
}

// TestSpecOnlyPredicateIsBlankLineProof drives the predicate against literal file lists rather than
// against whatever the local git emits — the only way to assert a shape this machine's git does not
// produce. Skips rather than passing if the entry point is gone, because a silent pass here would be
// the same false green the shipped self-test used to give.
func TestSpecOnlyPredicateIsBlankLineProof(t *testing.T) {
	probeGit(t)
	script := prAuthorsScript(t)

	usage := exec.Command("bash", script)
	if out, _ := usage.CombinedOutput(); !strings.Contains(string(out), "--is-spec-only") {
		t.Skipf("SKIPPING, NOT PASSING: the installed pr-authors.sh has no `--is-spec-only` entry "+
			"point, so the predicate could not be driven against literal file lists and nothing "+
			"about it was determined here. Its usage is: %s", strings.TrimSpace(string(out)))
	}

	// rc 0 means "spec-only, confers no authorship"; rc 1 means "someone authored something".
	cases := []struct {
		in   string
		want int
	}{
		{"openspec/a.md\n", 0},
		{"\nopenspec/a.md\n", 0}, // the runner's shape — this is the outage
		{"openspec/a.md\n\n", 0}, //
		{"\n\nopenspec/a.md\nopenspec/b.md\n", 0},
		{"   \nopenspec/a.md\n", 0}, // whitespace is not a path either
		{"internal/a.go\n", 1},
		{"\ninternal/a.go\n", 1},
		{"\nopenspec/a.md\ninternal/a.go\n", 1}, // a commit NAMED archive that carries code
		{"\n", 0},                               // a commit that changed nothing
		{"", 0},
	}
	for _, c := range cases {
		cmd := exec.Command("bash", script, "--is-spec-only")
		cmd.Stdin = strings.NewReader(c.in)
		out, err := cmd.CombinedOutput()
		got := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("cannot run %s --is-spec-only: %v\n%s", script, err, out)
			}
			got = ee.ExitCode()
		}
		if got != c.want {
			t.Errorf("--is-spec-only on %q exited %d, want %d.\n"+
				"A BLANK LINE IS NOT A PATH. `git show --name-only` is porcelain and promises "+
				"nothing about blank lines; if one is allowed to fail the ^openspec/ test then an "+
				"archive commit confers authorship wherever that shape appears (Issue #61).\n%s",
				c.in, got, c.want, out)
		}
	}
}

// TestArchiveOnlyPullRequestCanBeReviewed is the SECOND defect, and it is a different one.
//
// It has nothing to do with git versions: it reproduces with the exemption working perfectly. A pull
// request whose every commit is under `openspec/` correctly has every author stripped, and the
// review gate then read that empty set as "no commit carries an `Agent:` trailer" — about commits
// that plainly carry one. The trailer is present; the AUTHOR SET is empty; those are two different
// facts and rendering them identically made #63 unmergeable except with `--admin`.
//
// So the fix is not in the predicate at all — it is `--all-trailers`, a second question the caller
// must ask to tell "nobody authored product judgement here" from "who built this cannot be
// determined". This test asserts the outcome a person cares about: a reviewer can clear an
// archive-only pull request.
func TestArchiveOnlyPullRequestCanBeReviewed(t *testing.T) {
	p := probeGit(t)
	needTool(t, "jq")

	f := newAuthorFixture(t, p.path)
	f.commit("chore(x): archive the change", "product", "openspec/specs/x/spec.md")

	if got := f.authors(t, "", nil); len(got) != 0 {
		t.Errorf("an archive-only branch reported the authors %v. Nobody authored product judgement "+
			"there — every commit changed only openspec/ — so every role is independent.", got)
	}
	if got := f.authors(t, "", []string{"--all-trailers"}); strings.Join(got, ",") != "product" {
		t.Errorf("`--all-trailers` answered %v, want [product]. WITHOUT THIS THE CALLER CANNOT TELL "+
			"TWO DIFFERENT FACTS APART: an empty author set means either that no commit carries an "+
			"`Agent:` trailer — a defect in the commits — or that every commit was spec-only, which "+
			"is a determined answer and not a defect at all. Collapsing them refused #63, an "+
			"archive-only pull request, with 'no commit carries an Agent: trailer' about a commit "+
			"carrying `Agent: product`.", got)
	}

	// AND THE OUTCOME, THROUGH THE REAL GATE. Any role is independent here, so a review by any of
	// them must be accepted.
	if rc, out := f.checkReview(t, "dev", "approve"); rc != 0 {
		t.Errorf("check-review.sh exited %d for an archive-only pull request approved by an "+
			"independent reviewer, want 0. NO REVIEWER CAN EVER CLEAR SUCH A PULL REQUEST while "+
			"this is red, and it merges only with --admin.\n%s", rc, out)
	}

	// A BRANCH WITH NO TRAILER AT ALL IS STILL REFUSED, or the distinction is a difference in name
	// only and the gate has simply been widened.
	g := newAuthorFixture(t, p.path)
	g.commit("chore(x): archive with no trailer", "", "openspec/specs/x/spec.md")
	if got := g.authors(t, "", []string{"--all-trailers"}); len(got) != 0 {
		t.Errorf("`--all-trailers` reported %v for commits that carry no `Agent:` trailer at all", got)
	}
	rc, out := g.checkReview(t, "dev", "approve")
	if rc == 0 {
		t.Errorf("check-review.sh accepted a branch whose commits carry no `Agent:` trailer. Who "+
			"built it cannot be determined, and 'could not be determined' is not 'determined to be "+
			"nobody'.\n%s", out)
	}
	if !strings.Contains(out, "Agent:") {
		t.Errorf("the refusal does not name the missing `Agent:` trailer, so a reviewer reading it "+
			"will look for a fault in its own verdict instead of in the commits:\n%s", out)
	}
}

// checkReview runs the installed review gate over the fixture with one review comment for its head.
func (f *authorFixture) checkReview(t *testing.T, reviewer, verdict string) (int, string) {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".workflow", "bin", "check-review.sh")
	// The `[role]` marker is what the gate takes the reviewer's identity FROM since Issue #65 — a
	// verdict with no poster is refused as unattributable, so a fixture without one would be
	// testing that refusal rather than whatever this caller is asking about.
	body := "[" + reviewer + "]" +
		"\\nReviewed-by: " + reviewer + "\\nReviewed-sha: " + f.head + "\\nVerdict: " + verdict +
		"\\nScope: the change is what the Issue asked for and no wider."
	f.write("comments.json", `[{"body":"`+body+`"}]`)

	cmd := exec.Command("bash", script, f.head, "comments.json", f.base)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("cannot run %s: %v\n%s", script, err, out)
		}
		rc = ee.ExitCode()
	}
	return rc, string(out)
}

// TestPrAuthorsSelfTestPasses runs the installed script's own self-test. It cannot observe git
// changing under it — every fixture it builds uses the same git — which is why the tests above
// exist; but a script that cannot verify itself has no working derivation of authorship at all, and
// that is worth learning from `make ci` rather than from a withdrawn review.
func TestPrAuthorsSelfTestPasses(t *testing.T) {
	probeGit(t)
	out, err := exec.Command("bash", prAuthorsScript(t), "--self-test").CombinedOutput()
	if err != nil {
		t.Fatalf("pr-authors.sh --self-test failed: %v\n%s", err, out)
	}
}
