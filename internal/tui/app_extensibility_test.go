package tui

import (
	"errors"
	"reflect"
	"testing"

	gitpkg "github.com/dc/codereview/internal/git"
)

type fakeGitService struct {
	diffOut     string
	diffErr     error
	diffCalls   int
	diffRevSpec string

	diffFullOut     string
	diffFullErr     error
	diffFullCalls   int
	diffFullRevSpec string

	logOut   []gitpkg.CommitInfo
	logErr   error
	logCalls int
	logN     int

	untrackedOut   []string
	untrackedErr   error
	untrackedCalls int
}

func (f *fakeGitService) Diff(revSpec string) (string, error) {
	f.diffCalls++
	f.diffRevSpec = revSpec
	return f.diffOut, f.diffErr
}

func (f *fakeGitService) DiffFull(revSpec string) (string, error) {
	f.diffFullCalls++
	f.diffFullRevSpec = revSpec
	return f.diffFullOut, f.diffFullErr
}

func (f *fakeGitService) Log(n int) ([]gitpkg.CommitInfo, error) {
	f.logCalls++
	f.logN = n
	return f.logOut, f.logErr
}

func (f *fakeGitService) UntrackedFiles() ([]string, error) {
	f.untrackedCalls++
	return f.untrackedOut, f.untrackedErr
}

func TestNewModelDefaultsGitService(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	if m.git == nil {
		t.Fatal("git service is nil; expected default adapter")
	}
	if _, ok := m.git.(defaultGitService); !ok {
		t.Fatalf("default git service type = %T, want %T", m.git, defaultGitService{})
	}
}

func TestLoadDiffUsesInjectedGitService(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{
		diffOut: "",
		logOut:  []gitpkg.CommitInfo{{Hash: "abc123", Subject: "test commit"}},
	}
	m := NewModel(Config{RevSpec: "HEAD~1", Git: fake})

	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}
	if fake.diffCalls != 1 {
		t.Fatalf("Diff call count = %d, want 1", fake.diffCalls)
	}
	if fake.diffRevSpec != "HEAD~1" {
		t.Fatalf("Diff revSpec = %q, want %q", fake.diffRevSpec, "HEAD~1")
	}
	if fake.logCalls != 1 {
		t.Fatalf("Log call count = %d, want 1", fake.logCalls)
	}
	if fake.logN != 50 {
		t.Fatalf("Log n = %d, want 50", fake.logN)
	}
	if fake.untrackedCalls != 1 {
		t.Fatalf("UntrackedFiles call count = %d, want 1", fake.untrackedCalls)
	}
	if !reflect.DeepEqual(msg.commits, fake.logOut) {
		t.Fatalf("commits = %#v, want %#v", msg.commits, fake.logOut)
	}
}

func TestLoadDiffPropagatesDiffError(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{diffErr: errors.New("diff failed")}
	m := NewModel(Config{Git: fake})

	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err == nil {
		t.Fatal("expected loadDiff error, got nil")
	}
	if msg.err.Error() != "diff failed" {
		t.Fatalf("error = %q, want %q", msg.err.Error(), "diff failed")
	}
	if fake.logCalls != 0 {
		t.Fatalf("Log call count = %d, want 0 when Diff fails", fake.logCalls)
	}
	if fake.untrackedCalls != 0 {
		t.Fatalf("UntrackedFiles call count = %d, want 0 when Diff fails", fake.untrackedCalls)
	}
}

func TestLoadDiffLogFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{
		diffOut: "",
		logErr:  errors.New("log failed"),
	}
	m := NewModel(Config{Git: fake})

	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}
	if fake.diffCalls != 1 {
		t.Fatalf("Diff call count = %d, want 1", fake.diffCalls)
	}
	if fake.logCalls != 1 {
		t.Fatalf("Log call count = %d, want 1", fake.logCalls)
	}
	if fake.untrackedCalls != 1 {
		t.Fatalf("UntrackedFiles call count = %d, want 1", fake.untrackedCalls)
	}
}

func TestApplyDiffLoadedMarksUntrackedFiles(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{
		diffOut: `diff --git a/u.txt b/u.txt
new file mode 100644
index 0000000..c1b0730
--- /dev/null
+++ b/u.txt
@@ -0,0 +1 @@
+hello
`,
		untrackedOut: []string{"u.txt"},
	}
	m := NewModel(Config{Git: fake})

	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}
	m.applyDiffLoaded(msg)
	if got := len(m.fileList.files); got != 1 {
		t.Fatalf("file count = %d, want 1", got)
	}
	if !m.fileList.files[0].Untracked {
		t.Fatal("expected file to be marked untracked")
	}
}
