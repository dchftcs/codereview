package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	gitpkg "github.com/dc/codereview/internal/git"
)

func TestUpdateLayoutEnforcesMinimumContentHeight(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	m.width = 100
	m.height = 8

	m.updateLayout()

	if m.diffView.height != minContentHeight {
		t.Fatalf("diffView.height = %d, want %d", m.diffView.height, minContentHeight)
	}
	if m.fileList.height != minContentHeight {
		t.Fatalf("fileList.height = %d, want %d", m.fileList.height, minContentHeight)
	}
}

func TestRenderHeaderAlwaysSingleLine(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	m.commits = []gitpkg.CommitInfo{{
		Hash:    "abc1234",
		Subject: strings.Repeat("long subject ", 20),
	}}

	m.width = 80
	if h := lipgloss.Height(m.renderHeader()); h != 1 {
		t.Fatalf("header height at width 80 = %d, want 1", h)
	}

	m.width = 0
	if h := lipgloss.Height(m.renderHeader()); h != 1 {
		t.Fatalf("header height at width 0 = %d, want 1", h)
	}
}

func TestViewHeightStableForShortAndFullDiffs(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	m.width = 100
	m.height = 30

	commits := []gitpkg.CommitInfo{{
		Hash:    "abc1234",
		Subject: strings.Repeat("subject ", 40),
	}}

	shortFile := *makeLargeFileDiff(3)
	m.applyDiffLoaded(diffLoadedMsg{
		files:   []diff.FileDiff{shortFile},
		commits: commits,
	})

	shortView := m.View()
	shortHeight := lipgloss.Height(shortView)
	if shortHeight != m.height {
		t.Fatalf("short diff view height = %d, want %d", shortHeight, m.height)
	}
	shortFirstLine := stripAnsi(strings.Split(shortView, "\n")[0])
	if !strings.Contains(shortFirstLine, "[abc1234]") {
		t.Fatalf("short diff header missing commit hash: %q", shortFirstLine)
	}

	fullFile := *makeLargeFileDiff(300)
	m.applyDiffLoaded(diffLoadedMsg{
		files:   []diff.FileDiff{fullFile},
		commits: commits,
	})

	fullView := m.View()
	fullHeight := lipgloss.Height(fullView)
	if fullHeight != m.height {
		t.Fatalf("full diff view height = %d, want %d", fullHeight, m.height)
	}
	fullFirstLine := stripAnsi(strings.Split(fullView, "\n")[0])
	if !strings.Contains(fullFirstLine, "[abc1234]") {
		t.Fatalf("full diff header missing commit hash: %q", fullFirstLine)
	}
}
