package tui

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dc/codereview/internal/diff"
	gitpkg "github.com/dc/codereview/internal/git"
)

type fakeGitService struct {
	diffOut          string
	diffCollapsed    []gitpkg.CollapsedDir
	diffErr          error
	diffCalls        int
	diffRevSpec      string

	diffFullOut          string
	diffFullCollapsed    []gitpkg.CollapsedDir
	diffFullErr          error
	diffFullCalls        int
	diffFullRevSpec      string

	diffUnstagedOut          string
	diffUnstagedCollapsed    []gitpkg.CollapsedDir
	diffUnstagedErr          error
	diffUnstagedCalls        int

	diffUnstagedFullOut          string
	diffUnstagedFullCollapsed    []gitpkg.CollapsedDir
	diffUnstagedFullErr          error
	diffUnstagedFullCalls        int

	logOut   []gitpkg.CommitInfo
	logErr   error
	logCalls int
	logN     int

	statusOut   []gitpkg.FileStatus
	statusErr   error
	statusCalls int

	stageErr     error
	stageCalls   int
	stagePath    string
	unstageErr   error
	unstageCalls int
	unstagePath  string

	untrackedOut   []string
	untrackedErr   error
	untrackedCalls int

	listFilesOut   []string
	listFilesErr   error
	listFilesCalls int

	readFileOut   []byte
	readFileErr   error
	readFilePath  string
	readFileCalls int

	repoRoot       string
	displayRoot    string
	localWorktree  bool
	pollInterval   time.Duration
}

func (f *fakeGitService) Diff(revSpec string) (string, []gitpkg.CollapsedDir, error) {
	f.diffCalls++
	f.diffRevSpec = revSpec
	return f.diffOut, f.diffCollapsed, f.diffErr
}

func (f *fakeGitService) DiffFull(revSpec string) (string, []gitpkg.CollapsedDir, error) {
	f.diffFullCalls++
	f.diffFullRevSpec = revSpec
	return f.diffFullOut, f.diffFullCollapsed, f.diffFullErr
}

func (f *fakeGitService) DiffUnstaged() (string, []gitpkg.CollapsedDir, error) {
	f.diffUnstagedCalls++
	return f.diffUnstagedOut, f.diffUnstagedCollapsed, f.diffUnstagedErr
}

func (f *fakeGitService) DiffUnstagedFull() (string, []gitpkg.CollapsedDir, error) {
	f.diffUnstagedFullCalls++
	return f.diffUnstagedFullOut, f.diffUnstagedFullCollapsed, f.diffUnstagedFullErr
}

func (f *fakeGitService) Log(n int) ([]gitpkg.CommitInfo, error) {
	f.logCalls++
	f.logN = n
	return f.logOut, f.logErr
}

func (f *fakeGitService) Status() ([]gitpkg.FileStatus, error) {
	f.statusCalls++
	return f.statusOut, f.statusErr
}

func (f *fakeGitService) Stage(path string) error {
	f.stageCalls++
	f.stagePath = path
	return f.stageErr
}

func (f *fakeGitService) Unstage(path string) error {
	f.unstageCalls++
	f.unstagePath = path
	return f.unstageErr
}

func (f *fakeGitService) UntrackedFiles() ([]string, error) {
	f.untrackedCalls++
	return f.untrackedOut, f.untrackedErr
}

func (f *fakeGitService) ListFiles() ([]string, error) {
	f.listFilesCalls++
	return f.listFilesOut, f.listFilesErr
}

func (f *fakeGitService) ReadFile(path string) ([]byte, error) {
	f.readFileCalls++
	f.readFilePath = path
	return f.readFileOut, f.readFileErr
}

func (f *fakeGitService) RepoRoot() string {
	if f.repoRoot != "" {
		return f.repoRoot
	}
	return "."
}

func (f *fakeGitService) DisplayRoot() string {
	if f.displayRoot != "" {
		return f.displayRoot
	}
	return f.RepoRoot()
}

func (f *fakeGitService) HasLocalWorkingTree() bool {
	return f.localWorktree
}

func (f *fakeGitService) StatusPollInterval() time.Duration {
	if f.pollInterval > 0 {
		return f.pollInterval
	}
	return gitpkg.DefaultStatusPollInterval
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
	if fake.statusCalls != 1 {
		t.Fatalf("Status call count = %d, want 1", fake.statusCalls)
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

func TestLoadDiffUnstagedUsesInjectedGitService(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{
		diffUnstagedOut: "",
		logOut:          []gitpkg.CommitInfo{{Hash: "abc123", Subject: "test commit"}},
	}
	m := NewModel(Config{UnstagedOnly: true, Git: fake})

	msg := m.loadDiff()().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadDiff returned error: %v", msg.err)
	}
	if fake.diffUnstagedCalls != 1 {
		t.Fatalf("DiffUnstaged call count = %d, want 1", fake.diffUnstagedCalls)
	}
	if fake.diffCalls != 0 {
		t.Fatalf("Diff call count = %d, want 0 in unstaged mode", fake.diffCalls)
	}
	if fake.statusCalls != 1 {
		t.Fatalf("Status call count = %d, want 1", fake.statusCalls)
	}
}

func TestLoadExpandedDiffUnstagedUsesInjectedGitService(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{
		diffUnstagedFullOut: "",
	}
	m := NewModel(Config{UnstagedOnly: true, Git: fake})

	msg := m.loadExpandedDiff()().(expandLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadExpandedDiff returned error: %v", msg.err)
	}
	if fake.diffUnstagedFullCalls != 1 {
		t.Fatalf("DiffUnstagedFull call count = %d, want 1", fake.diffUnstagedFullCalls)
	}
	if fake.diffFullCalls != 0 {
		t.Fatalf("DiffFull call count = %d, want 0 in unstaged mode", fake.diffFullCalls)
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
	if fake.statusCalls != 0 {
		t.Fatalf("Status call count = %d, want 0 when Diff fails", fake.statusCalls)
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
	if fake.statusCalls != 1 {
		t.Fatalf("Status call count = %d, want 1", fake.statusCalls)
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

func TestApplyDiffLoadedMarksStagedFiles(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{
		diffOut: `diff --git a/u.txt b/u.txt
index 7898192..c1b0730 100644
--- a/u.txt
+++ b/u.txt
@@ -1 +1 @@
-before
+after
`,
		statusOut: []gitpkg.FileStatus{{Path: "u.txt", Index: 'M', Worktree: ' '}},
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
	if !m.fileList.files[0].Staged {
		t.Fatal("expected file to be marked staged")
	}
}

func TestToggleStageStagesSelectedFile(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{}
	m := NewModel(Config{Git: fake})
	m.width = 120
	m.height = 30
	m.fileList = newFileList([]diff.FileDiff{{OldName: "a.go", NewName: "a.go"}})
	m.fileList.review = m.review
	m.updateLayout()

	next, cmd := m.updateNormal(keyRunes("a"))
	updated := next.(Model)
	if cmd == nil {
		t.Fatal("expected toggle-stage command")
	}

	msg := cmd().(stageToggledMsg)
	if msg.err != nil {
		t.Fatalf("toggle stage returned error: %v", msg.err)
	}
	if fake.stageCalls != 1 {
		t.Fatalf("Stage call count = %d, want 1", fake.stageCalls)
	}
	if fake.stagePath != "a.go" {
		t.Fatalf("Stage path = %q, want %q", fake.stagePath, "a.go")
	}
	if msg.preserveSelection != "a.go" {
		t.Fatalf("preserveSelection = %q, want %q", msg.preserveSelection, "a.go")
	}
	if updated.statusMsg != "" {
		t.Fatalf("statusMsg before async completion = %q, want empty", updated.statusMsg)
	}
}

func TestToggleStageUnstagesSelectedFile(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{}
	m := NewModel(Config{Git: fake})
	m.width = 120
	m.height = 30
	m.fileList = newFileList([]diff.FileDiff{{OldName: "a.go", NewName: "a.go", Staged: true}})
	m.fileList.review = m.review
	m.updateLayout()

	_, cmd := m.updateNormal(keyRunes("a"))
	if cmd == nil {
		t.Fatal("expected toggle-stage command")
	}

	msg := cmd().(stageToggledMsg)
	if msg.err != nil {
		t.Fatalf("toggle stage returned error: %v", msg.err)
	}
	if fake.unstageCalls != 1 {
		t.Fatalf("Unstage call count = %d, want 1", fake.unstageCalls)
	}
	if fake.unstagePath != "a.go" {
		t.Fatalf("Unstage path = %q, want %q", fake.unstagePath, "a.go")
	}
}

func TestStatusPolledUpdatesMarkersWithoutReload(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	m.width = 120
	m.height = 30
	m.fileList = newFileList([]diff.FileDiff{{OldName: "a.go", NewName: "a.go"}})
	m.fileList.review = m.review
	m.statusSnapshot = map[string]statusSnapshotEntry{
		"a.go": {Index: ' ', Worktree: 'M', Staged: false},
	}
	m.statusAppliedEpoch = 1
	m.updateLayout()

	next, cmd := m.Update(statusPolledMsg{
		epoch: 2,
		snapshot: map[string]statusSnapshotEntry{
			"a.go": {Index: 'M', Worktree: 'M', Staged: true},
		},
	})
	updated := next.(Model)
	if cmd != nil {
		t.Fatal("expected no full reload for marker-only change")
	}
	if !updated.fileList.files[0].Staged {
		t.Fatal("expected staged marker to update in place")
	}
	if !updated.statusSnapshot["a.go"].Staged {
		t.Fatal("expected authoritative snapshot to update")
	}
}

func TestStatusPolledTriggersReloadOnStructuralChange(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{diffOut: ""}
	m := NewModel(Config{Git: fake})
	m.width = 120
	m.height = 30
	m.fileList = newFileList([]diff.FileDiff{{OldName: "a.go", NewName: "a.go"}})
	m.fileList.review = m.review
	m.statusSnapshot = map[string]statusSnapshotEntry{
		"a.go": {Index: ' ', Worktree: 'M', Staged: false},
	}
	m.statusAppliedEpoch = 1
	m.updateLayout()

	next, cmd := m.Update(statusPolledMsg{
		epoch: 2,
		snapshot: map[string]statusSnapshotEntry{
			"a.go": {Index: ' ', Worktree: 'M', Staged: false},
			"b.go": {Index: 'M', Worktree: ' ', Staged: true},
		},
	})
	updated := next.(Model)
	if cmd == nil {
		t.Fatal("expected full reload when status file set changes")
	}
	if updated.statusAppliedEpoch != 2 {
		t.Fatalf("statusAppliedEpoch = %d, want 2", updated.statusAppliedEpoch)
	}
	msg := cmd().(diffLoadedMsg)
	if msg.err != nil {
		t.Fatalf("reload returned error: %v", msg.err)
	}
	if fake.diffCalls != 1 {
		t.Fatalf("Diff call count = %d, want 1", fake.diffCalls)
	}
}

func TestStatusPolledIgnoresStaleEpoch(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	m.statusSnapshot = map[string]statusSnapshotEntry{
		"a.go": {Index: 'M', Worktree: ' ', Staged: true},
	}
	m.statusAppliedEpoch = 3
	m.statusPollInFlight = true

	next, cmd := m.Update(statusPolledMsg{
		epoch: 2,
		snapshot: map[string]statusSnapshotEntry{
			"a.go": {Index: ' ', Worktree: 'M', Staged: false},
		},
	})
	updated := next.(Model)
	if cmd != nil {
		t.Fatal("expected no command for stale poll result")
	}
	if !updated.statusSnapshot["a.go"].Staged {
		t.Fatal("stale poll result should not overwrite snapshot")
	}
	if updated.statusPollInFlight {
		t.Fatal("poll in-flight flag should be cleared on receipt")
	}
}

func TestStageToggledMsgOptimisticallyUpdatesSnapshot(t *testing.T) {
	t.Parallel()

	fake := &fakeGitService{diffOut: ""}
	m := NewModel(Config{Git: fake})
	m.width = 120
	m.height = 30
	m.fileList = newFileList([]diff.FileDiff{{OldName: "a.go", NewName: "a.go"}})
	m.fileList.review = m.review
	m.updateLayout()

	next, cmd := m.Update(stageToggledMsg{
		action:            "Staged a.go",
		path:              "a.go",
		staged:            true,
		preserveSelection: "a.go",
	})
	updated := next.(Model)
	if !updated.fileList.files[0].Staged {
		t.Fatal("expected optimistic staged marker update")
	}
	if !updated.statusSnapshot["a.go"].Staged {
		t.Fatal("expected optimistic snapshot update")
	}
	if cmd == nil {
		t.Fatal("expected follow-up diff reload")
	}
}
