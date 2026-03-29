package git

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type CommitInfo struct {
	Hash    string
	Subject string
}

// CollapsedDir represents a directory with many untracked files that was
// collapsed instead of diffing each file individually.
type CollapsedDir struct {
	Dir   string
	Count int
}

type FileStatus struct {
	Path      string
	Index     byte
	Worktree  byte
	Original  string
	RenamedTo string
}

// Log returns recent commits as a list.
func Log(n int) ([]CommitInfo, error) {
	out, err := runNoLock("log", "--oneline", fmt.Sprintf("-n%d", n), "--no-decorate")
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		ci := CommitInfo{Hash: parts[0]}
		if len(parts) > 1 {
			ci.Subject = parts[1]
		}
		commits = append(commits, ci)
	}
	return commits, nil
}

// maxIndividualUntracked is the threshold above which GroupUntrackedFiles
// starts collapsing the largest directories to keep load times reasonable.
const maxIndividualUntracked = 100

// GroupUntrackedFiles splits a list of untracked file paths into files that
// should be diffed individually and directories that should be collapsed.
// If the total count is <= maxIndividual, everything is individual.
// Otherwise, files are grouped by top-level directory and the largest
// directories are collapsed until the remaining count fits.
func GroupUntrackedFiles(files []string, maxIndividual int) (individual []string, collapsed []CollapsedDir) {
	if len(files) <= maxIndividual {
		return files, nil
	}

	// Group by top-level directory component. Root-level files are always individual.
	dirCounts := make(map[string][]string)
	var rootFiles []string
	for _, f := range files {
		slash := strings.IndexByte(f, '/')
		if slash < 0 {
			rootFiles = append(rootFiles, f)
		} else {
			dir := f[:slash]
			dirCounts[dir] = append(dirCounts[dir], f)
		}
	}

	// Sort directories by file count descending so we collapse the biggest first.
	type dirEntry struct {
		dir   string
		files []string
	}
	dirs := make([]dirEntry, 0, len(dirCounts))
	for d, fs := range dirCounts {
		dirs = append(dirs, dirEntry{dir: d, files: fs})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i].files) > len(dirs[j].files)
	})

	remaining := len(files)
	collapsedSet := make(map[string]bool)
	for _, d := range dirs {
		if remaining <= maxIndividual {
			break
		}
		collapsedSet[d.dir] = true
		collapsed = append(collapsed, CollapsedDir{Dir: d.dir, Count: len(d.files)})
		remaining -= len(d.files)
	}

	individual = rootFiles
	for _, d := range dirs {
		if collapsedSet[d.dir] {
			continue
		}
		individual = append(individual, d.files...)
	}
	return individual, collapsed
}

// Diff returns the unified diff for the given revision spec.
// If revSpec is empty, returns uncommitted changes (working tree vs HEAD).
// Supports two-dot (a..b) and three-dot (a...b) syntax.
func Diff(revSpec string) (string, []CollapsedDir, error) {
	return diffInternal(revSpec, false)
}

// DiffFull returns the diff with full file context (all lines visible).
func DiffFull(revSpec string) (string, []CollapsedDir, error) {
	return diffInternal(revSpec, true)
}

// DiffUnstaged returns only unstaged tracked changes plus untracked files.
func DiffUnstaged() (string, []CollapsedDir, error) {
	return diffUnstagedInternal(false)
}

// DiffUnstagedFull returns only unstaged tracked changes plus untracked files with full context.
func DiffUnstagedFull() (string, []CollapsedDir, error) {
	return diffUnstagedInternal(true)
}

func diffInternal(revSpec string, fullContext bool) (string, []CollapsedDir, error) {
	ctx := []string{}
	if fullContext {
		ctx = []string{"-U99999"}
	}
	if revSpec == "" {
		out, err := run(append([]string{"diff"}, append(ctx, "HEAD")...)...)
		if err != nil {
			return "", nil, err
		}
		return appendUntrackedDiff(out, fullContext)
	}
	// Three-dot: diff from merge-base (branch changes only)
	if strings.Contains(revSpec, "...") {
		parts := strings.SplitN(revSpec, "...", 2)
		base, head := parts[0], parts[1]
		mergeBase, err := MergeBase(base, head)
		if err != nil {
			return "", nil, fmt.Errorf("finding merge base: %w", err)
		}
		// For default branch-vs-HEAD review, include committed + staged + unstaged
		// changes by diffing merge-base directly to the working tree.
		if head == "HEAD" {
			out, err := run(append([]string{"diff"}, append(ctx, mergeBase)...)...)
			if err != nil {
				return "", nil, err
			}
			return appendUntrackedDiff(out, fullContext)
		}
		out, err := run(append([]string{"diff"}, append(ctx, mergeBase, head)...)...)
		return out, nil, err
	}
	// Two-dot range
	if strings.Contains(revSpec, "..") {
		out, err := run(append([]string{"diff"}, append(ctx, revSpec)...)...)
		return out, nil, err
	}
	// Single commit — use show to handle root commits (no parent)
	args := append([]string{"show", "--format="}, append(ctx, revSpec)...)
	out, err := run(args...)
	return out, nil, err
}

func diffUnstagedInternal(fullContext bool) (string, []CollapsedDir, error) {
	ctx := []string{}
	if fullContext {
		ctx = []string{"-U99999"}
	}
	out, err := run(append([]string{"diff"}, ctx...)...)
	if err != nil {
		return "", nil, err
	}
	return appendUntrackedDiff(out, fullContext)
}

// Show returns the diff for a single commit.
func Show(commit string) (string, error) {
	return run("show", "--format=", commit)
}

// IsRevision reports whether ref resolves to a commit.
func IsRevision(ref string) bool {
	if strings.TrimSpace(ref) == "" {
		return false
	}
	_, err := run("rev-parse", "--verify", ref+"^{commit}")
	return err == nil
}

// CurrentBranch returns the current branch name.
func CurrentBranch() (string, error) {
	out, err := run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// MergeBase returns the merge base (fork point) between two refs.
func MergeBase(a, b string) (string, error) {
	out, err := run("merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DefaultBranch returns "main" or "master", whichever exists.
func DefaultBranch() string {
	if out, err := run("rev-parse", "--verify", "main"); err == nil && strings.TrimSpace(out) != "" {
		return "main"
	}
	if out, err := run("rev-parse", "--verify", "master"); err == nil && strings.TrimSpace(out) != "" {
		return "master"
	}
	return "main"
}

// ListFiles returns all tracked files plus untracked non-ignored files
// (relative to repo root). This is equivalent to `git ls-files --cached --others --exclude-standard`.
func ListFiles() ([]string, error) {
	out, err := runNoLock("ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var files []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l)
	}
	return files, nil
}

// UntrackedFiles returns untracked file paths (relative to repo root).
func UntrackedFiles() ([]string, error) {
	out, err := runNoLock("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var files []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l)
	}
	return files, nil
}

// Status returns git porcelain status entries keyed to working-tree paths.
func Status() ([]FileStatus, error) {
	out, err := runNoLock("status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	return parsePorcelainStatus(out), nil
}

// Stage stages the provided path in the index.
func Stage(path string) error {
	_, err := run("add", "--", path)
	return err
}

// Unstage removes the provided path from the index while leaving the working tree intact.
func Unstage(path string) error {
	_, err := run("restore", "--staged", "--", path)
	return err
}

func parsePorcelainStatus(out string) []FileStatus {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	statuses := make([]FileStatus, 0, len(lines))
	for _, line := range lines {
		if len(line) < 3 || strings.TrimSpace(line) == "" {
			continue
		}
		entry := FileStatus{
			Index:    line[0],
			Worktree: line[1],
		}
		pathPart := line[3:]
		if strings.Contains(pathPart, " -> ") {
			parts := strings.SplitN(pathPart, " -> ", 2)
			entry.Original = parts[0]
			entry.Path = parts[1]
			entry.RenamedTo = parts[1]
		} else {
			entry.Path = pathPart
		}
		statuses = append(statuses, entry)
	}
	return statuses
}

func appendUntrackedDiff(base string, fullContext bool) (string, []CollapsedDir, error) {
	files, err := UntrackedFiles()
	if err != nil {
		return "", nil, err
	}
	if len(files) == 0 {
		return base, nil, nil
	}

	individual, collapsed := GroupUntrackedFiles(files, maxIndividualUntracked)

	var b strings.Builder
	b.WriteString(base)
	for _, p := range individual {
		args := []string{"diff", "--no-index"}
		if fullContext {
			args = append(args, "-U99999")
		}
		args = append(args, "--", "/dev/null", p)
		out, err := runAllowExitCode(args...)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(out) == "" {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(out)
	}
	return b.String(), collapsed, nil
}

func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
		}
		return "", err
	}
	return string(out), nil
}

// runNoLock runs a read-only git command with --no-optional-locks to avoid
// acquiring the index lock for stat-cache refreshes. This prevents lock
// contention between polling reads and index-modifying operations (add/restore).
func runNoLock(args ...string) (string, error) {
	return run(append([]string{"--no-optional-locks"}, args...)...)
}

func runAllowExitCode(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		// `git diff --no-index` exits 1 when differences are found.
		if exitErr.ExitCode() == 1 {
			return string(out), nil
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
	}
	return "", err
}
