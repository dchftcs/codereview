package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDiffWithRealRepo(t *testing.T) {
	ensureGit(t)

	repo := initTestRepo(t)
	commitTestFile(t, repo, "hello.go", "package hello\n", "initial")

	// Modify the file in the working tree.
	writeFile(t, repo, "hello.go", "package hello\n\n// changed\n")

	chdir(t, repo)

	m := NewModel(Config{})
	msg := m.loadDiff()().(diffLoadedMsg)

	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}
	if len(msg.files) == 0 {
		t.Fatal("loadDiff returned no files; expected modified hello.go")
	}

	var found bool
	for _, f := range msg.files {
		if f.NewName == "hello.go" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(msg.files))
		for i, f := range msg.files {
			names[i] = f.NewName
		}
		t.Fatalf("hello.go not in diff files: %v", names)
	}
}

func TestLoadDiffWithUntrackedFile(t *testing.T) {
	ensureGit(t)

	repo := initTestRepo(t)
	commitTestFile(t, repo, "seed.txt", "seed\n", "seed")

	// Create an untracked file.
	writeFile(t, repo, "newfile.go", "package newfile\n")

	chdir(t, repo)

	m := NewModel(Config{})
	msg := m.loadDiff()().(diffLoadedMsg)

	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}

	var found bool
	for _, f := range msg.files {
		if f.NewName == "newfile.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("untracked newfile.go not in diff files")
	}
}

func TestLoadDiffStagedAndUnstaged(t *testing.T) {
	ensureGit(t)

	repo := initTestRepo(t)
	commitTestFile(t, repo, "a.go", "package a\n", "initial")

	// Commit b.go first so the staging of a.go isn't included in that commit.
	commitTestFile(t, repo, "b.go", "package b\n", "add b")

	// Stage a modification to a.go.
	writeFile(t, repo, "a.go", "package a\n// staged\n")
	gitRun(t, repo, "add", "a.go")

	// Also add an unstaged change to b.go.
	writeFile(t, repo, "b.go", "package b\n// unstaged\n")

	chdir(t, repo)

	m := NewModel(Config{})
	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}

	foundA, foundB := false, false
	for _, f := range msg.files {
		if f.NewName == "a.go" {
			foundA = true
		}
		if f.NewName == "b.go" {
			foundB = true
		}
	}
	if !foundA {
		t.Fatal("staged file a.go not in diff")
	}
	if !foundB {
		t.Fatal("unstaged file b.go not in diff")
	}
}

func TestApplyDiffLoadedPopulatesFileList(t *testing.T) {
	ensureGit(t)

	repo := initTestRepo(t)
	commitTestFile(t, repo, "main.go", "package main\n", "initial")
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, repo, "util.go", "package main\n// new util\n")

	chdir(t, repo)

	m := NewModel(Config{})
	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}

	m.width = 120
	m.height = 40
	m.applyDiffLoaded(msg)

	if len(m.fileList.files) == 0 {
		t.Fatal("applyDiffLoaded resulted in empty file list")
	}

	names := make([]string, len(m.fileList.files))
	for i, f := range m.fileList.files {
		names[i] = f.NewName
	}

	if !contains(names, "main.go") {
		t.Fatalf("file list missing main.go: %v", names)
	}
	if !contains(names, "util.go") {
		t.Fatalf("file list missing util.go: %v", names)
	}
}

func TestLoadDiffCommitRevSpec(t *testing.T) {
	ensureGit(t)

	repo := initTestRepo(t)
	commitTestFile(t, repo, "a.txt", "one\n", "first")
	hash := commitTestFile(t, repo, "a.txt", "two\n", "second")

	chdir(t, repo)

	m := NewModel(Config{RevSpec: hash})
	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}
	if len(msg.files) != 1 {
		t.Fatalf("expected 1 file in commit diff, got %d", len(msg.files))
	}
	if msg.files[0].NewName != "a.txt" {
		t.Fatalf("expected a.txt, got %s", msg.files[0].NewName)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// --- test helpers ---

func ensureGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "--initial-branch", "main")
	gitRun(t, repo, "config", "user.name", "Test User")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	return repo
}

func commitTestFile(t *testing.T, repo, relPath, content, message string) string {
	t.Helper()
	writeFile(t, repo, relPath, content)
	gitRun(t, repo, "add", relPath)
	gitRun(t, repo, "commit", "-m", message)
	return strings.TrimSpace(gitRun(t, repo, "rev-parse", "HEAD"))
}

func writeFile(t *testing.T, repo, relPath, content string) {
	t.Helper()
	full := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", relPath, err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func gitRun(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
