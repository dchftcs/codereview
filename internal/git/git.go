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
func Diff(revSpec string) (string, error) {
	if revSpec == "" {
		return run("diff", "HEAD")
	}
	// Check if it's a range (contains ..)
	if strings.Contains(revSpec, "..") {
		return run("diff", revSpec)
	}
	// Single commit — show its diff
	return run("diff", revSpec+"~1", revSpec)
}

// DiffUnstaged returns diff of uncommitted changes (both staged and unstaged vs HEAD).
func DiffUnstaged() (string, error) {
	return run("diff", "HEAD")
}

// Show returns the diff for a single commit.
func Show(commit string) (string, error) {
	return run("show", "--format=", commit)
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
