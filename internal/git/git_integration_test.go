package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBranchDetectsMainAndMaster(t *testing.T) {
	ensureGitAvailable(t)

	t.Run("main", func(t *testing.T) {
		repo := initRepo(t, "main")
		commitFile(t, repo, "seed.txt", "seed\n", "seed")
		chdir(t, repo)
		if got := DefaultBranch(); got != "main" {
			t.Fatalf("DefaultBranch() = %q, want %q", got, "main")
		}
	})

	t.Run("master fallback", func(t *testing.T) {
		repo := initRepo(t, "master")
		commitFile(t, repo, "seed.txt", "seed\n", "seed")
		if strings.TrimSpace(gitCmd(t, repo, "branch", "--list", "main")) != "" {
			gitCmd(t, repo, "branch", "-D", "main")
		}
		chdir(t, repo)
		if got := DefaultBranch(); got != "master" {
			t.Fatalf("DefaultBranch() = %q, want %q", got, "master")
		}
	})
}

func TestMergeBaseOnDivergedBranches(t *testing.T) {
	ensureGitAvailable(t)

	repo := initRepo(t, "main")
	base := commitFile(t, repo, "a.txt", "base\n", "base")
	gitCmd(t, repo, "checkout", "-b", "feature")
	commitFile(t, repo, "feature.txt", "feature\n", "feature")
	gitCmd(t, repo, "checkout", "main")
	commitFile(t, repo, "main.txt", "main\n", "main")

	chdir(t, repo)
	mb, err := MergeBase("main", "feature")
	if err != nil {
		t.Fatalf("MergeBase returned error: %v", err)
	}
	if mb != base {
		t.Fatalf("merge base = %q, want %q", mb, base)
	}
}

func TestDiffVariants(t *testing.T) {
	ensureGitAvailable(t)

	repo := initRepo(t, "main")
	base := commitFile(t, repo, "demo.txt", "one\ntwo\nthree\n", "base")
	gitCmd(t, repo, "checkout", "-b", "feature")
	featureCommit := commitFile(t, repo, "demo.txt", "one\nfeature-two\nthree\n", "feature change")
	gitCmd(t, repo, "checkout", "main")
	commitFile(t, repo, "demo.txt", "main-one\ntwo\nthree\n", "main change")

	chdir(t, repo)
	threeDot, err := Diff("main...feature")
	if err != nil {
		t.Fatalf("Diff(main...feature) returned error: %v", err)
	}
	if !strings.Contains(threeDot, "feature-two") || strings.Contains(threeDot, "main-one") {
		t.Fatalf("unexpected three-dot diff output:\n%s", threeDot)
	}

	twoDot, err := Diff(base + ".." + featureCommit)
	if err != nil {
		t.Fatalf("Diff(two-dot) returned error: %v", err)
	}
	if !strings.Contains(twoDot, "feature-two") {
		t.Fatalf("two-dot diff missing feature content:\n%s", twoDot)
	}

	single, err := Diff(featureCommit)
	if err != nil {
		t.Fatalf("Diff(single commit) returned error: %v", err)
	}
	if !strings.Contains(single, "feature-two") {
		t.Fatalf("single-commit diff missing feature content:\n%s", single)
	}

	if err := os.WriteFile(filepath.Join(repo, "demo.txt"), []byte("one\nfeature-two\nthree\nworking-tree\n"), 0644); err != nil {
		t.Fatalf("WriteFile working tree change: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked-line\n"), 0644); err != nil {
		t.Fatalf("WriteFile untracked change: %v", err)
	}
	working, err := Diff("")
	if err != nil {
		t.Fatalf("Diff(\"\") returned error: %v", err)
	}
	if !strings.Contains(working, "working-tree") {
		t.Fatalf("working-tree diff missing change:\n%s", working)
	}
	if !strings.Contains(working, "untracked-line") {
		t.Fatalf("working-tree diff missing untracked file:\n%s", working)
	}
}

func TestDiffFullHasMoreContext(t *testing.T) {
	ensureGitAvailable(t)

	repo := initRepo(t, "main")
	baseContent := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"
	commitFile(t, repo, "ctx.txt", baseContent, "base")
	changed := "l1\nl2\nl3\nl4\nCHANGED\nl6\nl7\nl8\nl9\nl10\n"
	target := commitFile(t, repo, "ctx.txt", changed, "change middle")

	chdir(t, repo)
	normal, err := Diff(target)
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	full, err := DiffFull(target)
	if err != nil {
		t.Fatalf("DiffFull returned error: %v", err)
	}
	if strings.Count(full, "\n") <= strings.Count(normal, "\n") {
		t.Fatalf("DiffFull should include more context; normal lines=%d full lines=%d", strings.Count(normal, "\n"), strings.Count(full, "\n"))
	}
	if !strings.Contains(full, "l1") || !strings.Contains(full, "l10") {
		t.Fatalf("DiffFull missing full-context lines:\n%s", full)
	}
}

func TestThreeDotHeadIncludesWorkingTreeAndStagedChanges(t *testing.T) {
	ensureGitAvailable(t)

	repo := initRepo(t, "main")
	commitFile(t, repo, "demo.txt", "base\n", "base")
	gitCmd(t, repo, "checkout", "-b", "feature")
	commitFile(t, repo, "demo.txt", "base\nfeature-commit\n", "feature commit")

	// Unstaged working-tree change
	if err := os.WriteFile(filepath.Join(repo, "demo.txt"), []byte("base\nfeature-commit\nworking-tree\n"), 0644); err != nil {
		t.Fatalf("WriteFile working tree change: %v", err)
	}
	// Staged change in another file
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged-change\n"), 0644); err != nil {
		t.Fatalf("WriteFile staged file: %v", err)
	}
	gitCmd(t, repo, "add", "staged.txt")
	// Untracked file
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked-change\n"), 0644); err != nil {
		t.Fatalf("WriteFile untracked file: %v", err)
	}

	chdir(t, repo)
	out, err := Diff("main...HEAD")
	if err != nil {
		t.Fatalf("Diff(main...HEAD) returned error: %v", err)
	}
	if !strings.Contains(out, "feature-commit") {
		t.Fatalf("expected committed branch change in output:\n%s", out)
	}
	if !strings.Contains(out, "working-tree") {
		t.Fatalf("expected unstaged change in output:\n%s", out)
	}
	if !strings.Contains(out, "staged-change") {
		t.Fatalf("expected staged change in output:\n%s", out)
	}
	if !strings.Contains(out, "untracked-change") {
		t.Fatalf("expected untracked change in output:\n%s", out)
	}
}

func TestLogParsesCountAndFormat(t *testing.T) {
	ensureGitAvailable(t)

	repo := initRepo(t, "main")
	commitFile(t, repo, "a.txt", "1\n", "one")
	commitFile(t, repo, "a.txt", "2\n", "two")
	commitFile(t, repo, "a.txt", "3\n", "three")

	chdir(t, repo)
	commits, err := Log(2)
	if err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("len(Log(2)) = %d, want 2", len(commits))
	}
	for i, c := range commits {
		if c.Hash == "" || c.Subject == "" {
			t.Fatalf("commit[%d] parsed unexpectedly: %+v", i, c)
		}
	}
}

func TestParsePorcelainStatus(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"M  staged.txt",
		" M unstaged.txt",
		"R  old.txt -> new.txt",
		"?? untracked.txt",
	}, "\n")

	got := parsePorcelainStatus(out)
	if len(got) != 4 {
		t.Fatalf("len(parsePorcelainStatus) = %d, want 4", len(got))
	}
	if got[0].Path != "staged.txt" || got[0].Index != 'M' || got[0].Worktree != ' ' {
		t.Fatalf("unexpected staged entry: %+v", got[0])
	}
	if got[2].Path != "new.txt" || got[2].Original != "old.txt" || got[2].RenamedTo != "new.txt" {
		t.Fatalf("unexpected rename entry: %+v", got[2])
	}
	if got[3].Path != "untracked.txt" || got[3].Index != '?' || got[3].Worktree != '?' {
		t.Fatalf("unexpected untracked entry: %+v", got[3])
	}
}

func ensureGitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
}

func initRepo(t *testing.T, initialBranch string) string {
	t.Helper()
	repo := t.TempDir()
	gitCmd(t, repo, "init", "--initial-branch", initialBranch)
	gitCmd(t, repo, "config", "user.name", "Test User")
	gitCmd(t, repo, "config", "user.email", "test@example.com")
	return repo
}

func commitFile(t *testing.T, repo, relPath, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, relPath), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", relPath, err)
	}
	gitCmd(t, repo, "add", relPath)
	gitCmd(t, repo, "commit", "-m", message)
	return strings.TrimSpace(gitCmd(t, repo, "rev-parse", "HEAD"))
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
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func gitCmd(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
