package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
)

func TestUpdateNavigationChangesCursorAndFileSelection(t *testing.T) {
	t.Parallel()

	m := newTestModel()

	if m.fileList.selected != 0 {
		t.Fatalf("initial selected file = %d, want 0", m.fileList.selected)
	}

	m = pressKey(m, keyRunes("]"))
	if m.fileList.selected != 1 {
		t.Fatalf("selected file after ] = %d, want 1", m.fileList.selected)
	}

	m = pressKey(m, keyRunes("2"))
	m = pressKey(m, keyRunes("["))
	if m.fileList.selected != 0 {
		t.Fatalf("selected file after 2[ = %d, want 0", m.fileList.selected)
	}

	m = pressKey(m, keyRunes("["))
	if m.fileList.selected != 0 {
		t.Fatalf("selected file after [ = %d, want 0", m.fileList.selected)
	}

	m = pressKey(m, keyRunes("2"))
	m = pressKey(m, keyRunes("j"))
	if m.diffView.cursorY < 2 {
		t.Fatalf("cursor after 2j = %d, want >=2", m.diffView.cursorY)
	}

	m = pressKey(m, keyRunes("g"))
	m = pressKey(m, keyRunes("g"))
	if m.diffView.cursorY != 0 {
		t.Fatalf("cursor after gg = %d, want 0", m.diffView.cursorY)
	}

	m = pressKey(m, keyRunes("G"))
	if m.diffView.cursorY != len(m.diffView.rows)-1 {
		t.Fatalf("cursor after G = %d, want %d", m.diffView.cursorY, len(m.diffView.rows)-1)
	}

	m = pressKey(m, keyRunes("2"))
	m = pressKey(m, keyRunes("G"))
	if m.diffView.cursorY != 1 {
		t.Fatalf("cursor after 2G = %d, want 1", m.diffView.cursorY)
	}
}

func TestArrowNavigationSkipsReadFilesButBracketNavigationDoesNot(t *testing.T) {
	t.Parallel()

	m := newTestModelWithFiles([]diff.FileDiff{
		{OldName: "a.go", NewName: "a.go"},
		{OldName: "b.go", NewName: "b.go"},
		{OldName: "c.go", NewName: "c.go"},
	})

	// Mark b.go as read.
	m = pressKey(m, keyRunes("]"))
	m = pressKey(m, keyRunes("m"))

	// ] should still include read files.
	m = pressKey(m, keyRunes("["))
	m = pressKey(m, keyRunes("]"))
	if got := m.fileList.selectedDiffPath(); got != "b.go" {
		t.Fatalf("selected file after ] = %q, want b.go", got)
	}

	// Right arrow should skip read b.go and go from a.go to c.go.
	m = pressKey(m, keyRunes("["))
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyRight})
	if got := m.fileList.selectedDiffPath(); got != "c.go" {
		t.Fatalf("selected file after right arrow = %q, want c.go", got)
	}

	// Left arrow should skip read b.go and return from c.go to a.go.
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	if got := m.fileList.selectedDiffPath(); got != "a.go" {
		t.Fatalf("selected file after left arrow = %q, want a.go", got)
	}
}

func TestFileListContextMenuMarkReadUnread(t *testing.T) {
	t.Parallel()

	m := newTestModelWithFiles([]diff.FileDiff{
		{OldName: "a.go", NewName: "a.go"},
		{OldName: "b.go", NewName: "b.go"},
	})

	headerH := lipgloss.Height(m.renderHeader())
	next, _ := m.handleRightClick(1, headerH+1)
	m = next.(Model)

	if m.mode != modeContextMenu {
		t.Fatalf("mode after right click = %v, want %v", m.mode, modeContextMenu)
	}
	if got := len(m.ctxMenu.items); got != 1 {
		t.Fatalf("len(ctxMenu.items) = %d, want 1", got)
	}
	if m.ctxMenu.items[0].label != "Mark read" {
		t.Fatalf("unexpected context menu items: %+v", m.ctxMenu.items)
	}

	m.ctxMenu.selected = 0
	next, _ = m.executeContextMenuItem()
	m = next.(Model)
	if !m.fileList.isPathRead("a.go") {
		t.Fatal("a.go should be marked read")
	}

	next, _ = m.handleRightClick(1, headerH+1)
	m = next.(Model)
	if m.ctxMenu.items[0].label != "Mark unread" {
		t.Fatalf("expected Mark unread after read state, got: %+v", m.ctxMenu.items)
	}
	m.ctxMenu.selected = 0
	next, _ = m.executeContextMenuItem()
	m = next.(Model)
	if m.fileList.isPathRead("a.go") {
		t.Fatal("a.go should be marked unread")
	}
}

func TestFooterNavigationHintsAdaptToFileMode(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.width = 260
	changedFooter := stripAnsi(m.renderFooter())
	if !strings.Contains(changedFooter, "`←/→`next/prev(unread)") {
		t.Fatalf("changed footer missing unread hint: %q", changedFooter)
	}
	if !strings.Contains(changedFooter, "`m`toggle read") {
		t.Fatalf("changed footer missing toggle-read hint: %q", changedFooter)
	}
	if strings.Contains(changedFooter, "`}/{`modified") {
		t.Fatalf("changed footer should not show modified hint: %q", changedFooter)
	}
	if strings.Contains(changedFooter, "`o`dir") {
		t.Fatalf("changed footer should not show dir hint: %q", changedFooter)
	}

	m.fileList.mode = fileListModeFullTree
	allFooter := stripAnsi(m.renderFooter())
	if !strings.Contains(allFooter, "`}/{`modified") {
		t.Fatalf("all-files footer missing modified hint: %q", allFooter)
	}
	if !strings.Contains(allFooter, "`o`dir") {
		t.Fatalf("all-files footer missing dir hint: %q", allFooter)
	}
}

func TestInlineCommentModeSubmitAndCancel(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = pressKey(m, keyRunes("j")) // move to first diff line (lineNum > 0)
	m = pressKey(m, keyRunes("c"))

	if m.mode != modeComment || !m.diffView.commentActive {
		t.Fatalf("expected inline comment mode active, mode=%v active=%v", m.mode, m.diffView.commentActive)
	}
	if strings.Contains(strings.ToLower(m.diffView.commentInput.Placeholder), "ctrl+s") {
		t.Fatalf("inline placeholder should not contain key hints: %q", m.diffView.commentInput.Placeholder)
	}
	inlineHeader := stripAnsi(m.diffView.renderInlineInput())
	if !strings.Contains(inlineHeader, "ctrl+s submit") {
		t.Fatalf("inline header missing key hint: %q", inlineHeader)
	}

	m = typeText(m, "looks good")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if m.mode != modeNormal {
		t.Fatalf("mode after submit = %v, want %v", m.mode, modeNormal)
	}
	if got := len(m.review.Comments); got != 1 {
		t.Fatalf("len(review.Comments) = %d, want 1", got)
	}
	if m.review.Comments[0].Text != "looks good" {
		t.Fatalf("comment text = %q, want %q", m.review.Comments[0].Text, "looks good")
	}

	m = pressKey(m, keyRunes("c"))
	m = typeText(m, "discard me")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeNormal || m.diffView.commentActive {
		t.Fatalf("expected canceled comment mode to return to normal")
	}
	if got := len(m.review.Comments); got != 1 {
		t.Fatalf("len(review.Comments) after cancel = %d, want 1", got)
	}
}

func TestViewportRelativeNavigationKeys(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.diffView.height = 12
	m.diffView.setFile(makeLargeFileDiff(80), m.review)
	m.diffView.scrollY = 20
	m.diffView.cursorY = 25
	viewport := m.diffView.contentViewportHeight()

	m = pressKey(m, keyRunes("H"))
	if m.diffView.cursorY != 20 {
		t.Fatalf("cursor after H = %d, want 20 (top visible row)", m.diffView.cursorY)
	}

	m = pressKey(m, keyRunes("L"))
	wantBottom := 20 + m.diffView.contentViewportHeight() - 1
	if wantBottom >= len(m.diffView.rows) {
		wantBottom = len(m.diffView.rows) - 1
	}
	if m.diffView.cursorY != wantBottom {
		t.Fatalf("cursor after L = %d, want %d (bottom visible row)", m.diffView.cursorY, wantBottom)
	}

	m.diffView.cursorY = 20
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.diffView.scrollY != 20+viewport {
		t.Fatalf("scroll after PgDn = %d, want %d", m.diffView.scrollY, 20+viewport)
	}

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.diffView.scrollY != 20 {
		t.Fatalf("scroll after PgUp = %d, want 20", m.diffView.scrollY)
	}
}

func TestCtrlFBCtrlBUseWindowScrollWithoutCursorAnchoring(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.diffView.height = 12
	m.diffView.setFile(makeLargeFileDiff(120), m.review)
	m.diffView.scrollY = 20
	m.diffView.cursorY = 25
	viewport := m.diffView.contentViewportHeight()

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.diffView.scrollY != 20+viewport {
		t.Fatalf("scroll after Ctrl+f = %d, want %d", m.diffView.scrollY, 20+viewport)
	}
	if m.diffView.cursorY < m.diffView.scrollY || m.diffView.cursorY >= m.diffView.scrollY+viewport {
		t.Fatalf("cursor after Ctrl+f = %d, expected within visible viewport [%d,%d)", m.diffView.cursorY, m.diffView.scrollY, m.diffView.scrollY+viewport)
	}

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.diffView.scrollY != 20 {
		t.Fatalf("scroll after Ctrl+b = %d, want 20", m.diffView.scrollY)
	}
	if m.diffView.cursorY < m.diffView.scrollY || m.diffView.cursorY >= m.diffView.scrollY+viewport {
		t.Fatalf("cursor after Ctrl+b = %d, expected within visible viewport [%d,%d)", m.diffView.cursorY, m.diffView.scrollY, m.diffView.scrollY+viewport)
	}
}

func TestCtrlFScrollPersistsAfterViewLayoutClamp(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.diffView.height = 12
	m.diffView.setFile(makeLargeFileDiff(120), m.review)
	m.diffView.scrollY = 20
	m.diffView.cursorY = 25
	viewport := m.diffView.contentViewportHeight()

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	_ = m.View() // triggers layout/clamp path

	if m.diffView.scrollY != 20+viewport {
		t.Fatalf("scroll after Ctrl+f + View = %d, want %d", m.diffView.scrollY, 20+viewport)
	}
}

func TestApplyDiffLoadedPathPrefixFilter(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{PathFilter: "internal/tui"})
	m.width = 120
	m.height = 30

	m.applyDiffLoaded(diffLoadedMsg{
		files: []diff.FileDiff{
			{OldName: "a.go", NewName: "a.go"},
			{OldName: "internal/tui/app.go", NewName: "internal/tui/app.go"},
			{OldName: "internal/git/git.go", NewName: "internal/git/git.go"},
		},
	})

	if got := len(m.fileList.files); got != 1 {
		t.Fatalf("filtered file count = %d, want 1", got)
	}
	if got := m.fileList.selectedDiffPath(); got != "internal/tui/app.go" {
		t.Fatalf("selected path = %q, want internal/tui/app.go", got)
	}
}

func TestApplyDiffLoadedGlobFilter(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{PathFilter: "internal/tui/*.go"})
	m.width = 120
	m.height = 30

	m.applyDiffLoaded(diffLoadedMsg{
		files: []diff.FileDiff{
			{OldName: "README.md", NewName: "README.md"},
			{OldName: "internal/tui/app.go", NewName: "internal/tui/app.go"},
			{OldName: "cmd/cr/main.go", NewName: "cmd/cr/main.go"},
		},
	})

	if got := len(m.fileList.files); got != 1 {
		t.Fatalf("filtered file count = %d, want 1", got)
	}
	if got := m.fileList.selectedDiffPath(); got != "internal/tui/app.go" {
		t.Fatalf("selected path = %q, want internal/tui/app.go", got)
	}
}

func TestMouseWheelScrollsDiffViewport(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.diffView.height = 12
	m.diffView.setFile(makeLargeFileDiff(120), m.review)
	m.diffView.scrollY = 10
	m.diffView.cursorY = 13

	headerH := lipgloss.Height(m.renderHeader())
	mouseY := headerH + 2
	mouseX := m.fileListWidth + 8

	next, _ := m.Update(tea.MouseMsg{
		X:      mouseX,
		Y:      mouseY,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	m = next.(Model)

	if m.diffView.scrollY != 13 {
		t.Fatalf("scrollY after wheel down = %d, want 13", m.diffView.scrollY)
	}
	if m.diffView.cursorY != 16 {
		t.Fatalf("cursorY after wheel down = %d, want 16", m.diffView.cursorY)
	}
}

func TestMouseWheelScrollsGeneralPanelWhenHovered(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.review.GeneralComments = []string{strings.Repeat("line\n", 12)}
	m.review.GeneralComments[0] += "line"
	m.generalPanel.review = m.review
	m.updateLayout()

	headerH := lipgloss.Height(m.renderHeader())
	bodyHeight := m.diffView.height + 2
	mouseY := headerH + bodyHeight + 1

	next, _ := m.Update(tea.MouseMsg{
		X:      4,
		Y:      mouseY,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	m = next.(Model)

	if m.focus != focusGeneralPanel {
		t.Fatalf("focus after wheel on general panel = %v, want %v", m.focus, focusGeneralPanel)
	}
	if m.generalPanel.scrollY == 0 {
		t.Fatal("general panel did not scroll on wheel input")
	}
}

func TestDoubleClickGeneralPanelOpensEditor(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.review.AddGeneralComment("existing general")
	m.generalPanel.review = m.review
	m.updateLayout()

	baseTime := time.Unix(1, 0)
	tick := 0
	m.now = func() time.Time {
		tick++
		// Keep clicks within double-click threshold.
		return baseTime.Add(time.Duration(tick*100) * time.Millisecond)
	}

	headerH := lipgloss.Height(m.renderHeader())
	bodyHeight := m.diffView.height + 2
	mouseY := headerH + bodyHeight + 1

	next, cmd := m.Update(tea.MouseMsg{
		X:      4,
		Y:      mouseY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = next.(Model)
	if cmd != nil {
		// Focus command is expected only on the second click.
		t.Fatal("unexpected command on first click")
	}
	if m.mode != modeNormal {
		t.Fatalf("mode after first click = %v, want %v", m.mode, modeNormal)
	}

	next, cmd = m.Update(tea.MouseMsg{
		X:      4,
		Y:      mouseY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = next.(Model)
	if m.mode != modeGeneralComment {
		t.Fatalf("mode after second click = %v, want %v", m.mode, modeGeneralComment)
	}
	if cmd == nil {
		t.Fatal("expected focus command on double click")
	}
	if got := m.generalInput.Value(); got != "existing general" {
		t.Fatalf("general input prefill = %q, want %q", got, "existing general")
	}
}

func TestGeneralCommentSearchAndDeleteEditFlows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = pressKey(m, keyRunes("j"))

	m = pressKey(m, keyRunes("R"))
	if m.mode != modeGeneralComment {
		t.Fatalf("mode after R = %v, want %v", m.mode, modeGeneralComment)
	}
	m = typeText(m, "overall thought")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := len(m.review.GeneralComments); got != 1 || m.review.GeneralComments[0] != "overall thought" {
		t.Fatalf("unexpected general comments: %+v", m.review.GeneralComments)
	}

	m = pressKey(m, keyRunes("c"))
	m = typeText(m, "old text")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := len(m.review.Comments); got != 1 {
		t.Fatalf("expected one inline comment, got %d", got)
	}

	m = pressKey(m, keyRunes("E"))
	if m.mode != modeComment || !m.diffView.commentEditing {
		t.Fatalf("expected edit mode; mode=%v editing=%v", m.mode, m.diffView.commentEditing)
	}
	m = typeText(m, " updated")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := len(m.review.Comments); got != 1 {
		t.Fatalf("expected edited comment to remain single entry, got %d", got)
	}
	if m.review.Comments[0].Text != "old text updated" {
		t.Fatalf("edited comment text = %q", m.review.Comments[0].Text)
	}

	m = pressKey(m, keyRunes("d"))
	if got := len(m.review.Comments); got != 0 {
		t.Fatalf("len(review.Comments) after delete = %d, want 0", got)
	}

	m = pressKey(m, keyRunes("/"))
	if m.mode != modeSearch {
		t.Fatalf("mode after / = %v, want %v", m.mode, modeSearch)
	}
	m = typeText(m, "beta")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeNormal {
		t.Fatalf("mode after search submit = %v, want %v", m.mode, modeNormal)
	}
	if m.searchTerm != "beta" || len(m.searchMatches) == 0 {
		t.Fatalf("search state unexpected: term=%q matches=%v", m.searchTerm, m.searchMatches)
	}

	m = pressKey(m, keyRunes("p"))
	if m.mode != modeContentSearch {
		t.Fatalf("mode after p = %v, want %v", m.mode, modeContentSearch)
	}
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Fatalf("mode after cancel content search = %v, want %v", m.mode, modeNormal)
	}
}

func TestGeneralPanelFocusAndActions(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = pressKey(m, keyRunes("R"))
	m = typeText(m, "initial general")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if m.focus != focusGeneralPanel {
		t.Fatalf("focus after ctrl+r = %v, want %v", m.focus, focusGeneralPanel)
	}

	m = pressKey(m, keyRunes("E"))
	if m.mode != modeGeneralComment {
		t.Fatalf("mode after E in general panel = %v, want %v", m.mode, modeGeneralComment)
	}
	m = typeText(m, " updated")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := m.review.GeneralComments[0]; got != "initial general updated" {
		t.Fatalf("edited general comment = %q", got)
	}

	m = pressKey(m, keyRunes("d"))
	if got := len(m.review.GeneralComments); got != 0 {
		t.Fatalf("len(review.GeneralComments) after panel delete = %d, want 0", got)
	}

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.focus != focusDiff {
		t.Fatalf("focus after esc = %v, want %v", m.focus, focusDiff)
	}
}

func TestREditsExistingGeneralComment(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.review.AddGeneralComment("existing text")

	m = pressKey(m, keyRunes("R"))
	if m.mode != modeGeneralComment {
		t.Fatalf("mode after R = %v, want %v", m.mode, modeGeneralComment)
	}
	if got := m.generalInput.Value(); got != "existing text" {
		t.Fatalf("general input prefill = %q, want %q", got, "existing text")
	}
}

func TestSaveAndQuitFlags(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = pressKey(m, keyRunes("s"))
	if !m.saving {
		t.Fatal("saving flag was not set by 's'")
	}

	m = newTestModel()
	m = pressKey(m, keyRunes("q"))
	if !m.quitting {
		t.Fatal("quitting flag was not set by 'q'")
	}
}

func TestSavePromptsForFilenameWhenConfigured(t *testing.T) {
	t.Parallel()

	m := newTestModelWithConfig(Config{PromptSaveAs: true})
	m = pressKey(m, keyRunes("s"))
	if m.mode != modeSaveAs {
		t.Fatalf("mode after s = %v, want %v", m.mode, modeSaveAs)
	}
	if got := m.bottomBar.value(); got != defaultReviewOutputFile {
		t.Fatalf("save prompt default filename = %q, want %q", got, defaultReviewOutputFile)
	}

	m = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.saving {
		t.Fatal("saving flag was not set after confirming save filename")
	}
	if got := m.GetOutputFile(); got != defaultReviewOutputFile {
		t.Fatalf("output file after save prompt = %q, want %q", got, defaultReviewOutputFile)
	}
}

func TestQuitWithUnsavedReviewPromptsToSave(t *testing.T) {
	t.Parallel()

	m := newTestModelWithConfig(Config{PromptSaveAs: true})
	m = pressKey(m, keyRunes("j"))
	m = pressKey(m, keyRunes("c"))
	m = typeText(m, "unsaved note")
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	m = pressKey(m, keyRunes("q"))
	if m.mode != modeQuitConfirm {
		t.Fatalf("mode after q with unsaved review = %v, want %v", m.mode, modeQuitConfirm)
	}
	if m.quitting {
		t.Fatal("should not quit immediately when unsaved review exists")
	}

	m = pressKey(m, keyRunes("y"))
	if m.mode != modeSaveAs {
		t.Fatalf("mode after choosing save = %v, want %v", m.mode, modeSaveAs)
	}
}

func newTestModel() Model {
	return newTestModelWithConfig(Config{})
}

func newTestModelWithConfig(cfg Config) Model {
	return newTestModelWithFilesAndConfig(defaultTestFiles(), cfg)
}

func newTestModelWithFiles(files []diff.FileDiff) Model {
	return newTestModelWithFilesAndConfig(files, Config{})
}

func newTestModelWithFilesAndConfig(files []diff.FileDiff, cfg Config) Model {
	m := NewModel(cfg)
	m.width = 120
	m.height = 30

	m.fileList = newFileList(files)
	m.fileList.review = m.review
	m.updateLayout()
	if f := m.fileList.selectedFile(); f != nil {
		m.diffView.setFile(f, m.review)
	}
	return m
}

func defaultTestFiles() []diff.FileDiff {
	return []diff.FileDiff{
		{
			OldName: "a.go",
			NewName: "a.go",
			Hunks: []diff.Hunk{{
				OldStart: 1,
				OldCount: 2,
				NewStart: 1,
				NewCount: 3,
				Pairs: []diff.LinePair{
					{Left: &diff.DiffLine{Op: diff.OpEqual, Content: "alpha", OldNum: 1, NewNum: 1}, Right: &diff.DiffLine{Op: diff.OpEqual, Content: "alpha", OldNum: 1, NewNum: 1}},
					{Left: &diff.DiffLine{Op: diff.OpDelete, Content: "beta", OldNum: 2}, Right: &diff.DiffLine{Op: diff.OpInsert, Content: "beta2", NewNum: 2}},
					{Right: &diff.DiffLine{Op: diff.OpInsert, Content: "gamma", NewNum: 3}},
				},
			}},
		},
		{
			OldName: "b.go",
			NewName: "b.go",
			Hunks: []diff.Hunk{{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Pairs:    []diff.LinePair{{Left: &diff.DiffLine{Op: diff.OpEqual, Content: "x", OldNum: 1, NewNum: 1}, Right: &diff.DiffLine{Op: diff.OpEqual, Content: "x", OldNum: 1, NewNum: 1}}},
			}},
		},
	}
}

func pressKey(m Model, msg tea.KeyMsg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
