package git

import (
	"fmt"
	"os/exec"
	"strings"
)

type CommitInfo struct {
	Hash    string
	Subject string
}

// Log returns recent commits as a list.
func Log(n int) ([]CommitInfo, error) {
	out, err := run("log", "--oneline", fmt.Sprintf("-n%d", n), "--no-decorate")
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

// Diff returns the unified diff for the given revision spec.
// If revSpec is empty, returns uncommitted changes (working tree vs HEAD).
// Supports two-dot (a..b) and three-dot (a...b) syntax.
func Diff(revSpec string) (string, error) {
	return diffInternal(revSpec, false)
}

// DiffFull returns the diff with full file context (all lines visible).
func DiffFull(revSpec string) (string, error) {
	return diffInternal(revSpec, true)
}

func diffInternal(revSpec string, fullContext bool) (string, error) {
	ctx := []string{}
	if fullContext {
		ctx = []string{"-U99999"}
	}
	if revSpec == "" {
		return run(append([]string{"diff"}, append(ctx, "HEAD")...)...)
	}
	// Three-dot: diff from merge-base (branch changes only)
	if strings.Contains(revSpec, "...") {
		parts := strings.SplitN(revSpec, "...", 2)
		base, head := parts[0], parts[1]
		mergeBase, err := MergeBase(base, head)
		if err != nil {
			return "", fmt.Errorf("finding merge base: %w", err)
		}
		return run(append([]string{"diff"}, append(ctx, mergeBase, head)...)...)
	}
	// Two-dot range
	if strings.Contains(revSpec, "..") {
		return run(append([]string{"diff"}, append(ctx, revSpec)...)...)
	}
	// Single commit — use show to handle root commits (no parent)
	args := append([]string{"show", "--format="}, append(ctx, revSpec)...)
	return run(args...)
}

// Show returns the diff for a single commit.
func Show(commit string) (string, error) {
	return run("show", "--format=", commit)
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
