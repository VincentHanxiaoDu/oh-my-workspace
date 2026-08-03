package projects

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// DefaultDepth is how many directory levels below a watched directory a default scan reaches.
//
// Issue #4's ruling: the reference project (`hxd_underpants`, config.py:38) caps at 4 and the owner
// ruled deeper, so the default here is 8 — criterion 15. It is a default and not a constant of the
// walk: criterion 15 also requires the limit be settable to another value, and that a smaller value
// demonstrably change what the listing reports for the same tree.
const DefaultDepth = 8

// DepthEnv names the environment variable that overrides DefaultDepth.
//
// The environment and not only a flag, because the daemon polls with no command line: criterion 14
// requires the CLI and the control API to agree, and they cannot agree about a tree if the depth
// limit is reachable from one surface only. Both read this.
const DepthEnv = "OMW_PROJECT_DEPTH"

// prunedNames are the directory names never descended into. Criterion 18.
//
// PRUNED, NOT FILTERED AFTERWARDS. The criterion says so explicitly and is drivable: a very deep or
// very large tree under `node_modules` must not be traversed at all. A post-filter gives the same
// listing and the wrong cost — on a real repository it is the difference between a poll that
// finishes inside the two-second interval and one that does not, which turns criterion 4 into a
// question about the person's disk.
//
// Taken verbatim from the reference implementation the owner named (`config.py:39-42`).
var prunedNames = map[string]bool{
	"node_modules": true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".git":         true,
	"dist":         true,
	"build":        true,
	".next":        true,
	"target":       true,
	".cache":       true,
	"vendor":       true,
}

// IsPruned reports whether a directory name is skipped wholesale: it is on the prune list, or it
// begins with a dot (the reference implementation's ingest.py:176-179, and criterion 18).
//
// Exported so a test can assert the policy against the criterion's own list rather than against
// this file's copy of it.
func IsPruned(name string) bool {
	return prunedNames[name] || strings.HasPrefix(name, ".")
}

// State is what is known about one project's directory at one moment.
//
// EVERY DETERMINATION THAT CAN FAIL IS A tri.Value. That is not decoration: criteria 8, 9, 10 and 20
// require missing, unreadable and empty to be three distinct renderings, and the only durable way to
// keep three renderings apart is to keep three values apart. A bool Present plus an int Files
// collapses "does not exist" and "exists and is empty" into `false, 0` and `true, 0`, at which point
// keeping them distinct in the output is one careless format string away from failing.
type State struct {
	// Present is whether the directory exists. No means missing (criterion 8). Undetermined means
	// the product could not find out — which is NOT missing (PRD §4.3).
	Present tri.Value `json:"present"`
	// Readable is whether the directory's contents could be examined. Yes only if the whole walk
	// completed within the limit without an unreadable portion; see PartiallyRead for the middle
	// case. Undetermined is criterion 10's marking: it exists, and its state could not be read.
	Readable tri.Value `json:"readable"`
	// Files is the number of files the scan reached. It is MEANINGLESS unless Readable is Yes or
	// PartiallyRead is true, and the renderer never prints it otherwise — a count printed beside a
	// missing directory is exactly criterion 9's forbidden "both render as 0 files".
	Files int `json:"files"`
	// DepthLimit is the limit that was in force for this scan, so a listing can say what the number
	// it truncated at actually was rather than making a person guess at the build's default.
	DepthLimit int `json:"depth_limit"`
	// DepthLimitReached is criterion 16: the walk stopped descending because it hit the limit, so
	// content below it is not reported. A truncated walk must never render as a complete one.
	DepthLimitReached bool `json:"depth_limit_reached"`
	// UnreadablePaths are directories inside the project that refused to be read, relative to the
	// project root. Criterion 21: the walk continues, the entry still appears, and it is
	// distinguishable from a walk that read everything.
	UnreadablePaths []string `json:"unreadable_paths,omitempty"`
	// IgnoreSource names what decided the exclusions: "git" inside a repository, "prune-list"
	// outside one (criterion 19). Reported so a person can tell which policy produced their listing;
	// the two give genuinely different answers for the same tree.
	IgnoreSource string `json:"ignore_source"`
	// DirsVisited is how many directories the walk actually entered. It exists for criterion 18,
	// which is drivable only by observing that a pruned tree was NOT traversed — an assertion about
	// the count of directories entered, which no rendering of the result can carry.
	DirsVisited int `json:"dirs_visited"`
}

// PartiallyRead reports whether the scan read some of the project but not all of it. Criterion 21.
func (s State) PartiallyRead() bool { return len(s.UnreadablePaths) > 0 }

// Empty reports whether the directory was determined to contain nothing.
//
// Undetermined unless the directory is present AND was fully readable: a walk that could not read
// part of the tree and found nothing in the rest has not determined that the project is empty, and
// saying so would be §4.3's "could not determine" rendered as "determined to be nothing" — the one
// collapse this project's conventions put first.
func (s State) Empty() tri.Value {
	if s.Present != tri.Yes || s.Readable != tri.Yes {
		return tri.Undetermined
	}
	if s.Files == 0 {
		return tri.Yes
	}
	return tri.No
}

// DepthFor resolves the effective depth limit from the environment. Criterion 15.
//
// An unparseable or non-positive value falls back to the default rather than scanning nothing: a
// typo in an environment variable must not silently turn every project into an empty one, which is
// the "determined to be nothing" failure again, arrived at by a different road.
func DepthFor(getenv func(string) string) int {
	if getenv == nil {
		return DefaultDepth
	}
	raw := strings.TrimSpace(getenv(DepthEnv))
	if raw == "" {
		return DefaultDepth
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return DefaultDepth
	}
	return n
}

// Scan determines the state of one directory, right now.
//
// It never returns an error. Every way this can go wrong is a fact about the project that the person
// must see — missing, unreadable, partially read — and an error return would let a caller drop the
// project from the listing, which criterion 8 forbids in as many words.
func Scan(path string, depth int) State {
	st := State{DepthLimit: depth, IgnoreSource: "prune-list"}

	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		st.Present = tri.No // determined: it is not there. Criterion 8.
		return st
	case err != nil:
		// Present is left Undetermined, which is its zero value. We could not find out whether the
		// directory is there, and that is not the same answer as "it is not there".
		return st
	case !info.IsDir():
		// A path that is not a directory is not a present project. Criterion 13's other half: if one
		// is somehow registered, it carries the missing marking rather than rendering as ordinary.
		st.Present = tri.No
		return st
	}
	st.Present = tri.Yes

	// Inside a git repository the ignore set is git's, not ours (criterion 19). We do not parse a
	// .gitignore anywhere — nested ignore files, negations and repository-level excludes that live
	// in no .gitignore are all cases a naive reader gets wrong, and git is the only thing that gets
	// them all right. If git is unavailable or fails, we fall through to the walk rather than
	// reporting nothing.
	if files, unreadable, ok := gitFiles(path, depth); ok {
		st.IgnoreSource = "git"
		st.Files = len(files)
		st.DepthLimitReached = gitReachedDepth(path, depth)
		st.DirsVisited = 1
		st.UnreadablePaths = unreadable
		// THE SAME RULE AS THE HAND-WRITTEN WALK, AND DELIBERATELY THE SAME THREE LINES. A partial
		// read leaves Readable undetermined; only a clean read is tri.Yes. This branch used to set
		// tri.Yes unconditionally, so inside a git repository — which is what most watched
		// directories ARE — a partially-read project rendered as a complete scan.
		if len(st.UnreadablePaths) > 0 {
			sort.Strings(st.UnreadablePaths)
			return st
		}
		st.Readable = tri.Yes
		return st
	}

	if !walkTree(path, path, 0, depth, &st) {
		// THE ROOT ITSELF COULD NOT BE READ. Readable stays Undetermined, which is criterion 10's
		// marking, and NOT the partially-read marking of criterion 21: nothing was read, so there is
		// no partial result, and the two must not render the same. Caught by a test that compared the
		// four outcomes pairwise — the first version recorded the root as an unreadable SUBpath and so
		// rendered criterion 10's case in criterion 21's words.
		st.DirsVisited = 1
		return st
	}
	if st.PartiallyRead() {
		// Readable stays Undetermined: part of this project was not read, so "readable" is not a
		// thing we determined about it. Criterion 21's "distinguishable from a complete scan" is
		// carried by UnreadablePaths, and this keeps a partial scan from also claiming Yes.
		sort.Strings(st.UnreadablePaths)
		return st
	}
	st.Readable = tri.Yes
	return st
}

// walkTree descends one level. depth is how many levels below the root we already are.
//
// Written by hand rather than with filepath.WalkDir because the criteria are about what the walk
// DOES, not only about what it returns: WalkDir cannot express "do not enter this directory at all"
// without also being the thing that stat'd it, and criterion 18 is drivable precisely by the
// directories entered. DirsVisited counts them, so the assertion is available to a test.
// It reports false ONLY when the project root itself could not be read, which is a different fact
// from an unreadable subdirectory and must not be recorded as one.
func walkTree(root, dir string, level, limit int, st *State) bool {
	st.DirsVisited++

	entries, err := os.ReadDir(dir)
	if err != nil {
		if level == 0 {
			return false // the root: criterion 10's case, handled by the caller.
		}
		rel, rerr := filepath.Rel(root, dir)
		if rerr != nil {
			rel = dir
		}
		st.UnreadablePaths = append(st.UnreadablePaths, rel)
		return true // criterion 21: this portion is unreadable, the rest of the walk continues.
	}

	for _, e := range entries {
		name := e.Name()
		switch {
		case e.Type()&os.ModeSymlink != 0:
			// CRITERION 17. Not followed, and not counted as a file either: a symlink is not content
			// of this project, it is a pointer at content that may live anywhere. Not descending is
			// also why a link pointing at its own ancestor terminates instead of looping — there is
			// no cycle to loop around if no link is ever entered.
			continue
		case e.IsDir():
			if IsPruned(name) {
				// Skipped WITHOUT reading it. Criterion 18: not walked and filtered afterwards.
				continue
			}
			if level+1 > limit {
				// CRITERION 16. Content below the limit is not reported, and the fact that it exists
				// and was not reported is recorded so the listing can say so. Set before returning:
				// a truncation we noticed but did not record renders as a complete walk.
				st.DepthLimitReached = true
				continue
			}
			walkTree(root, filepath.Join(dir, name), level+1, limit, st)
		default:
			if !IsPruned(name) {
				st.Files++
			}
		}
	}
	return true
}

// gitFiles asks git for the file set inside a repository, and for what git could NOT read.
// Criteria 19 and 21.
//
// `git ls-files --cached --others --exclude-standard` is git's own answer to "what is here that you
// are not ignoring": tracked files plus untracked-and-not-ignored ones, with nested ignore files,
// negation patterns, $GIT_DIR/info/exclude and the user's global excludesFile all already applied.
// This package parses no ignore file, here or anywhere.
//
// THE EXIT CODE IS NOT EVIDENCE OF A COMPLETE READ, and this function used to treat it as though it
// were. `git ls-files` WARNS on stderr about a directory it cannot open and still exits 0:
//
//	$ git ls-files --cached --others --exclude-standard   # exit 0
//	warning: could not open directory 'locked/': Permission denied
//	top.txt
//
// The previous version used cmd.Output(), which discards stderr, and inspected only the error. Git
// told us it could not read part of the tree and we threw the message away — then reported a
// complete scan. A zero exit from a tool that warns on stderr is evidence of no FATAL error, which
// is not the same claim.
//
// ANY STDERR OUTPUT MEANS THE READ WAS NOT CLEAN. The named paths are extracted for the person's
// benefit, but the decision does not depend on parsing succeeding: a warning this code does not
// recognise — a future git, a message this parser does not match — still leaves the project marked
// partially read rather than complete. Recognising the message is a nicety; noticing that there WAS
// one is the correctness.
//
// The prune list and the dot rule are applied ON TOP of git's answer, because criterion 18 is not
// scoped to outside a repository — a checked-in `vendor/` directory is still pruned.
//
// ok is false when this is not a repository, when git is not installed, or when git failed. The
// caller then walks. A false here is never reported to a person as an empty project.
func gitFiles(root string, limit int) (files []string, unreadable []string, ok bool) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, nil, false
	}
	// A fully-constructed environment: git must not read the developer's own config while deciding
	// what a test's temporary repository contains, and it must not be handed the ambient GIT_DIR of
	// whatever process invoked omw. LC_ALL is pinned so the warnings this parses do not change
	// language with the person's locale — though see above: an unparsed warning still counts.
	gitEnv := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + root,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"LC_ALL=C",
	}

	probe := exec.Command(git, "-C", root, "rev-parse", "--is-inside-work-tree")
	probe.Env = gitEnv
	if out, err := probe.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil, nil, false
	}

	ls := exec.Command(git, "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	ls.Env = gitEnv
	var stdout, stderr bytes.Buffer
	ls.Stdout, ls.Stderr = &stdout, &stderr
	if err := ls.Run(); err != nil {
		return nil, nil, false
	}

	for _, rel := range strings.Split(stdout.String(), "\x00") {
		if rel == "" {
			continue
		}
		if gitExcluded(rel) {
			continue
		}
		if depthOf(rel) > limit {
			continue // criterion 16: below the limit, so not reported.
		}
		files = append(files, rel)
	}
	return files, unreadableFromGitWarnings(stderr.String()), true
}

// unreadableFromGitWarnings turns git's stderr into the paths it could not read.
//
// A line it recognises yields the path. A line it does NOT recognise yields the line itself, so an
// unrecognised warning is still recorded as something git could not do — the entry is marked
// partially read either way, and the person sees git's own words rather than nothing.
// Exported for a test that drives the wordings directly — including ones this parser does not
// recognise, which is the case that must still be counted.
func UnreadableFromGitWarnings(stderr string) []string { return unreadableFromGitWarnings(stderr) }

func unreadableFromGitWarnings(stderr string) []string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if p, ok := pathInGitWarning(line); ok {
			out = append(out, p)
			continue
		}
		out = append(out, "git said: "+line)
	}
	return out
}

// pathInGitWarning pulls the quoted path out of a message like
//
//	warning: could not open directory 'locked/': Permission denied
func pathInGitWarning(line string) (string, bool) {
	i := strings.IndexByte(line, '\'')
	if i < 0 {
		return "", false
	}
	j := strings.IndexByte(line[i+1:], '\'')
	if j < 0 {
		return "", false
	}
	p := line[i+1 : i+1+j]
	if p == "" {
		return "", false
	}
	// Trimmed so it matches the form the hand-written walk records, and so the same directory does
	// not render two ways depending on which path produced the entry.
	return strings.TrimSuffix(p, "/"), true
}

// gitExcluded applies the prune list and the dot rule to a git-reported path. Criterion 18 on top of
// criterion 19: git decides the IGNORE set, this decides the PRUNE set, and they are different
// policies that both apply.
func gitExcluded(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts[:len(parts)-1] {
		if IsPruned(p) {
			return true
		}
	}
	return IsPruned(parts[len(parts)-1])
}

// gitReachedDepth reports whether git listed anything below the limit, which is criterion 16 for the
// repository path: something exists down there and this listing is not reporting it.
func gitReachedDepth(root string, limit int) bool {
	all, _, ok := gitFiles(root, 1<<30)
	if !ok {
		return false
	}
	for _, rel := range all {
		if depthOf(rel) > limit {
			return true
		}
	}
	return false
}

// depthOf is how many directory levels below the root a relative path sits. A file directly in the
// root is at 0, matching walkTree's level.
func depthOf(rel string) int { return strings.Count(rel, "/") }
