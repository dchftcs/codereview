package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dc/codereview/internal/diff"
)

func TestFileSearchScorePrefersFilename(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path   string
		needle string
		score  int
	}{
		{path: "pkg/config", needle: "config", score: 0},
		{path: "pkg/configuration.go", needle: "config", score: 1},
		{path: "pkg/myconfig.go", needle: "config", score: 2},
		{path: "config/pkg/file.go", needle: "config", score: 3},
		{path: "pkg/file.go", needle: "config", score: -1},
	}

	for _, tc := range cases {
		if got := fileSearchScore(tc.path, tc.needle); got != tc.score {
			t.Fatalf("fileSearchScore(%q, %q) = %d, want %d", tc.path, tc.needle, got, tc.score)
		}
	}
}

func TestSearchModifiedListPrioritizesBestFilenameMatch(t *testing.T) {
	t.Parallel()

	fl := newFileList([]diff.FileDiff{
		{NewName: "pkg/configuration.md"},
		{NewName: "app/service.yaml"},
		{NewName: "docs/config"},
	})
	fl.selected = 0

	found, err := fl.search("config")
	if err != nil {
		t.Fatalf("search returned error: %v", err)
	}
	if !found {
		t.Fatal("search did not find a match")
	}
	if fl.selected != 2 {
		t.Fatalf("selected index = %d, want 2 for exact basename match", fl.selected)
	}
}

func TestModifiedJumpsInTreeMode(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean("/tmp/repo")
	fl := fileList{
		mode:     fileListModeTree,
		repoRoot: repoRoot,
		files: []diff.FileDiff{
			{NewName: "a.go"},
			{NewName: "nested/b.go"},
		},
		modifiedIndex: map[string]int{
			"a.go":        0,
			"nested/b.go": 1,
		},
		treeRows: []treeRow{
			{node: &treeNode{name: "a.go", absPath: filepath.Join(repoRoot, "a.go")}, depth: 0},
			{node: &treeNode{name: "docs", absPath: filepath.Join(repoRoot, "docs"), isDir: true}, depth: 0},
			{node: &treeNode{name: "readme.md", absPath: filepath.Join(repoRoot, "docs", "readme.md")}, depth: 1},
			{node: &treeNode{name: "nested", absPath: filepath.Join(repoRoot, "nested"), isDir: true}, depth: 0},
			{node: &treeNode{name: "b.go", absPath: filepath.Join(repoRoot, "nested", "b.go")}, depth: 1},
		},
		treeSelected: 2,
	}

	if !fl.nextModified() {
		t.Fatal("nextModified returned false")
	}
	if fl.treeSelected != 4 {
		t.Fatalf("treeSelected after nextModified = %d, want 4", fl.treeSelected)
	}
	if fl.selected != 1 {
		t.Fatalf("selected diff index after nextModified = %d, want 1", fl.selected)
	}

	if !fl.prevModified() {
		t.Fatal("prevModified returned false")
	}
	if fl.treeSelected != 0 {
		t.Fatalf("treeSelected after prevModified = %d, want 0", fl.treeSelected)
	}
	if fl.selected != 0 {
		t.Fatalf("selected diff index after prevModified = %d, want 0", fl.selected)
	}

	fl.treeSelected = 4
	if !fl.firstModified() {
		t.Fatal("firstModified returned false")
	}
	if fl.treeSelected != 0 {
		t.Fatalf("treeSelected after firstModified = %d, want 0", fl.treeSelected)
	}
}

func TestSearchContentFindsModifiedFirst(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "a.txt"), "alpha\nneedle\n")
	mustWriteFile(t, filepath.Join(tmp, "b.txt"), "needle\n")

	fl := newFileList([]diff.FileDiff{
		{NewName: "a.txt"},
	})
	fl.repoRoot = tmp

	path, found, err := fl.searchContent("needle")
	if err != nil {
		t.Fatalf("searchContent returned error: %v", err)
	}
	if !found {
		t.Fatal("searchContent did not find match")
	}
	if path != "a.txt" {
		t.Fatalf("searchContent path = %q, want %q", path, "a.txt")
	}
}

func TestFocusPathSwitchesToTreeForReferenceFiles(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "dir", "ref.txt"), "ref")

	fl := newFileList([]diff.FileDiff{{NewName: "changed.txt"}})
	fl.repoRoot = tmp
	fl.root = &treeNode{
		name:     filepath.Base(tmp),
		absPath:  tmp,
		isDir:    true,
		expanded: true,
	}

	if err := fl.focusPath("dir/ref.txt"); err != nil {
		t.Fatalf("focusPath returned error: %v", err)
	}
	if !fl.isTreeMode() {
		t.Fatal("expected tree mode after focusing reference file")
	}
	path, isDir, ok := fl.selectedTreePath()
	if !ok || isDir {
		t.Fatalf("selectedTreePath ok=%v isDir=%v, want file", ok, isDir)
	}
	if path != "dir/ref.txt" {
		t.Fatalf("selected path = %q, want %q", path, "dir/ref.txt")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
