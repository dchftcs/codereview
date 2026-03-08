package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	gitpkg "github.com/dc/codereview/internal/git"
	"github.com/dc/codereview/internal/review"
)

type mode int

const (
	modeNormal mode = iota
	modeComment
	modeGeneralComment
	modeHelp
	modeSearch
	modeFileSearch
	modeContentSearch
	modeSaveAs
	modeQuitConfirm
	modeViewGeneral
	modeVisual
	modeContextMenu
)

// GitService is the narrow git surface needed by the TUI.
type GitService interface {
	Diff(revSpec string) (string, error)
	DiffFull(revSpec string) (string, error)
	Log(n int) ([]gitpkg.CommitInfo, error)
}

type defaultGitService struct{}

func (defaultGitService) Diff(revSpec string) (string, error) {
	return gitpkg.Diff(revSpec)
}

func (defaultGitService) DiffFull(revSpec string) (string, error) {
	return gitpkg.DiffFull(revSpec)
}

func (defaultGitService) Log(n int) ([]gitpkg.CommitInfo, error) {
	return gitpkg.Log(n)
}

// Config holds TUI startup configuration.
type Config struct {
	RevSpec      string
	OutputFile   string
	PromptSaveAs bool
	Highlight    func(filename, content string) string
	Theme        ThemeName
	Git          GitService
}

// SaveMsg is sent when the user wants to save the review.
type SaveMsg struct {
	Review *review.Review
	Output string
}

const minContentHeight = scrollMarginLines*2 + 1
const defaultReviewOutputFile = "REVIEW.md"

type Model struct {
	config         Config
	mode           mode
	width          int
	height         int
	fileListWidth  int
	fileList       fileList
	diffView       diffView
	bottomBar      bottomBarInput
	review         *review.Review
	commits        []gitpkg.CommitInfo
	git            GitService
	commitIdx      int
	expandedSet    map[int]bool    // per-file expand tracking
	expandedFiles  []diff.FileDiff // cached full-context diffs
	fileStates     map[string]fileState
	referenceFiles map[string]*diff.FileDiff
	// General comment viewer/editor
	generalViewIdx int           // selected index in general comments viewer
	generalInput   textarea.Model // multi-line textarea for general comments
	generalEditIdx int           // -1 for new comment, >=0 for editing existing
	// Vim-style navigation
	countBuf      string // accumulated digit prefix (e.g. "12" for 12j)
	pendingG      bool   // waiting for second 'g' keypress (gg)
	searchTerm    string // current search pattern
	searchMatches []int  // row indices matching the search
	searchIdx     int    // current position in searchMatches
	err           error
	statusMsg     string // transient status message shown in footer
	quitting      bool
	saving        bool
	dirty         bool
	// Context menu state
	ctxMenu contextMenu
	// Mouse drag state (for drag-to-select)
	mouseDrag struct {
		active   bool
		startRow int // diff row where the press began
	}
}

func NewModel(cfg Config) Model {
	if cfg.Git == nil {
		cfg.Git = defaultGitService{}
	}
	if cfg.Theme != "" {
		applyTheme(cfg.Theme)
	}
	dv := newDiffView()
	dv.highlight = cfg.Highlight
	m := Model{
		config:         cfg,
		fileListWidth:  30,
		review:         review.New(),
		diffView:       dv,
		bottomBar:      newBottomBarInput(),
		git:            cfg.Git,
		expandedSet:    make(map[int]bool),
		fileStates:     make(map[string]fileState),
		referenceFiles: make(map[string]*diff.FileDiff),
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return m.loadDiff()
}

type diffLoadedMsg struct {
	files   []diff.FileDiff
	commits []gitpkg.CommitInfo
	err     error
}

func (m Model) loadDiff() tea.Cmd {
	return func() tea.Msg {
		rawDiff, err := m.git.Diff(m.config.RevSpec)
		if err != nil {
			return diffLoadedMsg{err: err}
		}

		files, err := diff.Parse(rawDiff)
		if err != nil {
			return diffLoadedMsg{err: err}
		}

		commits, _ := m.git.Log(50)

		return diffLoadedMsg{files: files, commits: commits}
	}
}

type expandLoadedMsg struct {
	files []diff.FileDiff
	err   error
}

// editorFinishedMsg is sent when $EDITOR exits.
type editorFinishedMsg struct {
	tmpFile string
	err     error
}

// editorCmd returns the user's preferred editor command.
func editorCmd() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return "vi"
}

func (m Model) loadExpandedDiff() tea.Cmd {
	return func() tea.Msg {
		rawDiff, err := m.git.DiffFull(m.config.RevSpec)
		if err != nil {
			return expandLoadedMsg{err: err}
		}
		files, err := diff.Parse(rawDiff)
		if err != nil {
			return expandLoadedMsg{err: err}
		}
		return expandLoadedMsg{files: files}
	}
}

// displayFileAtIndex returns the right FileDiff for a modified file index,
// using expanded (full-context) data when available and active.
func displayFileAtIndex(files []diff.FileDiff, idx int, expandedSet map[int]bool, expandedFiles []diff.FileDiff) *diff.FileDiff {
	if idx < 0 || idx >= len(files) {
		return nil
	}
	f := &files[idx]
	expanded := expandedSet[idx]
	if !expanded || expandedFiles == nil {
		return f
	}
	name := f.NewName
	if name == "/dev/null" {
		name = f.OldName
	}
	for i := range expandedFiles {
		eName := expandedFiles[i].NewName
		if eName == "/dev/null" {
			eName = expandedFiles[i].OldName
		}
		if eName == name {
			return &expandedFiles[i]
		}
	}
	return f
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case diffLoadedMsg:
		m.applyDiffLoaded(msg)
		return m, nil

	case commitLoadedMsg:
		m.applyCommitLoaded(msg)
		return m, nil

	case expandLoadedMsg:
		m.applyExpandLoaded(msg)
		return m, nil

	case generalEditorFinishedMsg:
		defer os.Remove(msg.tmpFile)
		if msg.err != nil {
			m.err = msg.err
			m.mode = modeNormal
			return m, nil
		}
		content, err := os.ReadFile(msg.tmpFile)
		if err != nil {
			m.err = err
			m.mode = modeNormal
			return m, nil
		}
		text := strings.TrimRight(string(content), "\n")
		if text != "" {
			if m.generalEditIdx >= 0 {
				m.review.EditGeneralComment(m.generalEditIdx, text)
				m.statusMsg = "General comment updated"
			} else {
				m.review.AddGeneralComment(text)
				m.statusMsg = fmt.Sprintf("General comment added (%d total)", len(m.review.GeneralComments))
			}
			m.dirty = true
		}
		m.mode = modeNormal
		return m, nil

	case editorFinishedMsg:
		defer os.Remove(msg.tmpFile)
		if msg.err != nil {
			m.err = msg.err
			m.mode = modeNormal
			m.diffView.deactivateComment()
			return m, nil
		}
		content, err := os.ReadFile(msg.tmpFile)
		if err != nil {
			m.err = err
			m.mode = modeNormal
			m.diffView.deactivateComment()
			return m, nil
		}
		text := strings.TrimRight(string(content), "\n")
		if text != "" {
			if m.diffView.commentEditing {
				m.review.DeleteCommentRange(m.diffView.commentFile, m.diffView.commentLineNum, m.diffView.commentEndLine)
			}
			if m.diffView.commentEndLine > 0 {
				m.review.AddRangeComment(m.diffView.commentFile, m.diffView.commentLineNum, m.diffView.commentEndLine, text)
			} else {
				m.review.AddComment(m.diffView.commentFile, m.diffView.commentLineNum, text)
			}
			m.dirty = true
			m.diffView.deactivateComment()
			m.diffView.buildRows()
		} else {
			m.diffView.deactivateComment()
		}
		m.mode = modeNormal
		return m, nil

	case tea.MouseMsg:
		if m.mode == modeContextMenu {
			return m.updateContextMenuMouse(msg)
		}
		if msg.Button == tea.MouseButtonLeft {
			if msg.Action == tea.MouseActionPress {
				// Click cancels any existing visual selection
				if m.mode == modeVisual {
					m.mode = modeNormal
					m.diffView.selectionActive = false
				}
				if m.mode == modeNormal {
					if msg.Shift {
						return m.handleRightClick(msg.X, msg.Y)
					}
					return m.handleMouseClick(msg.X, msg.Y)
				}
			}
			if msg.Action == tea.MouseActionMotion {
				return m.handleMouseDrag(msg.X, msg.Y)
			}
			if msg.Action == tea.MouseActionRelease {
				m.mouseDrag.active = false
			}
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeContextMenu {
			return m.updateContextMenu(msg)
		}
		if m.mode == modeComment {
			return m.updateComment(msg)
		}
		if m.mode == modeGeneralComment {
			return m.updateGeneralComment(msg)
		}
		if m.mode == modeSearch {
			return m.updateSearch(msg)
		}
		if m.mode == modeFileSearch {
			return m.updateFileSearch(msg)
		}
		if m.mode == modeContentSearch {
			return m.updateContentSearch(msg)
		}
		if m.mode == modeSaveAs {
			return m.updateSaveAs(msg)
		}
		if m.mode == modeQuitConfirm {
			return m.updateQuitConfirm(msg)
		}
		if m.mode == modeHelp {
			return m.updateHelp(msg)
		}
		if m.mode == modeViewGeneral {
			return m.updateViewGeneral(msg)
		}
		if m.mode == modeVisual {
			return m.updateVisual(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m *Model) applyDiffLoaded(msg diffLoadedMsg) {
	if msg.err != nil {
		m.err = msg.err
		return
	}
	m.fileList = newFileList(msg.files)
	m.fileList.review = m.review
	m.commits = msg.commits
	m.updateLayout()
	m.setDiffViewForSelection(false)
}

func (m *Model) applyCommitLoaded(msg commitLoadedMsg) {
	if msg.err != nil {
		m.err = msg.err
		return
	}
	m.fileList = newFileList(msg.files)
	m.fileList.review = m.review
	m.expandedSet = make(map[int]bool)
	m.expandedFiles = nil
	m.fileStates = make(map[string]fileState)
	m.referenceFiles = make(map[string]*diff.FileDiff)
	m.updateLayout()
	m.setDiffViewForSelection(false)
}

func (m *Model) applyExpandLoaded(msg expandLoadedMsg) {
	if msg.err != nil {
		m.err = msg.err
		return
	}
	m.expandedFiles = msg.files
	m.setDiffViewForSelection(true)
}

func (m *Model) currentStateKey() string {
	return m.fileList.selectionStateKey()
}

func (m *Model) selectedDisplayFile() (*diff.FileDiff, error) {
	if _, idx, ok := m.fileList.modifiedSelection(); ok {
		return displayFileAtIndex(m.fileList.files, idx, m.expandedSet, m.expandedFiles), nil
	}
	path, isDir, ok := m.fileList.refSelection()
	if !ok || isDir {
		return nil, nil
	}
	return m.referenceFileDiff(path)
}

func (m *Model) setDiffViewForSelection(keepPosition bool) {
	f, err := m.selectedDisplayFile()
	if err != nil {
		m.err = err
		return
	}
	if f == nil {
		return
	}
	if fs, ok := m.fileStates[m.currentStateKey()]; ok {
		m.diffView.file = f
		m.diffView.comments = m.review
		m.diffView.restoreState(fs)
		return
	}
	if keepPosition {
		m.diffView.setFileKeepPosition(f, m.review)
	} else {
		m.diffView.setFile(f, m.review)
	}
}

func (m *Model) referenceFileDiff(path string) (*diff.FileDiff, error) {
	if f, ok := m.referenceFiles[path]; ok {
		return f, nil
	}

	absPath := filepath.Join(m.fileList.repoRoot, filepath.FromSlash(path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	fd := &diff.FileDiff{
		OldName: path,
		NewName: path,
		Binary:  bytes.IndexByte(content, 0) >= 0,
	}
	if !fd.Binary {
		text := strings.ReplaceAll(string(content), "\r\n", "\n")
		lines := strings.Split(text, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			pairs := make([]diff.LinePair, 0, len(lines))
			for i, line := range lines {
				lineNum := i + 1
				dl := &diff.DiffLine{
					Op:      diff.OpEqual,
					Content: line,
					OldNum:  lineNum,
					NewNum:  lineNum,
				}
				pairs = append(pairs, diff.LinePair{Left: dl, Right: dl})
			}
			fd.Hunks = []diff.Hunk{{
				OldStart: 1,
				OldCount: len(lines),
				NewStart: 1,
				NewCount: len(lines),
				Pairs:    pairs,
			}}
		}
	}

	m.referenceFiles[path] = fd
	return fd, nil
}

func (m *Model) currentReviewFileName() (string, bool) {
	path, _, ok := m.fileList.modifiedSelection()
	return path, ok
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = "" // clear transient status on any keypress
	k := msg.String()

	// Handle 'gg' sequence: second 'g' goes to top
	if m.pendingG {
		m.pendingG = false
		if k == "g" {
			count := m.consumeCount()
			if count > 1 {
				// [count]gg = go to row [count]-1
				m.diffView.moveCursorTo(count - 1)
			} else {
				m.diffView.moveCursorTo(0)
			}
			return m, nil
		}
		// Not 'g', fall through — clear countBuf too since 'g' consumed it
		m.countBuf = ""
	}

	// Accumulate digit prefix (1-9 start, 0 extends)
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && m.countBuf == "" {
		m.countBuf = k
		return m, nil
	}
	if len(k) == 1 && k[0] >= '0' && k[0] <= '9' && m.countBuf != "" {
		m.countBuf += k
		return m, nil
	}

	// First 'g' of a potential 'gg' — wait for next key
	if k == "g" {
		m.pendingG = true
		return m, nil
	}

	count := m.consumeCount()
	if k == "G" {
		if count > 1 {
			// [count]G = go to row [count]-1
			m.diffView.moveCursorTo(count - 1)
		} else {
			// G = go to last row
			m.diffView.moveCursorTo(len(m.diffView.rows) - 1)
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.Quit):
		if m.hasUnsavedReview() {
			m.mode = modeQuitConfirm
			m.bottomBar.activate("Unsaved review: save before quitting? [y]es/[n]o/[c]ancel")
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, keys.Down):
		m.diffView.moveCursor(count)
	case key.Matches(msg, keys.Up):
		m.diffView.moveCursor(-count)
	case key.Matches(msg, keys.ScreenTop):
		m.diffView.moveCursorToViewportTop()
	case key.Matches(msg, keys.ScreenBottom):
		m.diffView.moveCursorToViewportBottom()
	case key.Matches(msg, keys.FullPageDown):
		m.diffView.moveCursor(count * m.diffView.contentViewportHeight())
	case key.Matches(msg, keys.FullPageUp):
		m.diffView.moveCursor(-count * m.diffView.contentViewportHeight())
	case key.Matches(msg, keys.HalfPageDown):
		m.diffView.moveCursor(count * m.diffView.height / 2)
	case key.Matches(msg, keys.HalfPageUp):
		m.diffView.moveCursor(-count * m.diffView.height / 2)

	case key.Matches(msg, keys.NextFile):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		for i := 0; i < count; i++ {
			m.fileList.next()
		}
		m.setDiffViewForSelection(false)
	case key.Matches(msg, keys.PrevFile):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		for i := 0; i < count; i++ {
			m.fileList.prev()
		}
		m.setDiffViewForSelection(false)

	case key.Matches(msg, keys.NextModified):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		for i := 0; i < count; i++ {
			if !m.fileList.nextModified() {
				break
			}
		}
		m.setDiffViewForSelection(false)
	case key.Matches(msg, keys.PrevModified):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		for i := 0; i < count; i++ {
			if !m.fileList.prevModified() {
				break
			}
		}
		m.setDiffViewForSelection(false)
	case key.Matches(msg, keys.FirstModified):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		m.fileList.firstModified()
		m.setDiffViewForSelection(false)

	case key.Matches(msg, keys.ToggleFileTree):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		if err := m.fileList.toggleMode(); err != nil {
			m.err = err
			return m, nil
		}
		m.setDiffViewForSelection(false)

	case key.Matches(msg, keys.ToggleTreeDir):
		if err := m.fileList.toggleTreeExpand(); err != nil {
			m.err = err
			return m, nil
		}
		m.setDiffViewForSelection(false)

	case key.Matches(msg, keys.ShrinkPanel):
		m.fileListWidth -= 5
		if m.fileListWidth < 15 {
			m.fileListWidth = 15
		}
		m.updateLayout()
	case key.Matches(msg, keys.GrowPanel):
		maxW := m.width / 2
		m.fileListWidth += 5
		if m.fileListWidth > maxW {
			m.fileListWidth = maxW
		}
		m.updateLayout()

	case key.Matches(msg, keys.NextCommit):
		if cmd := m.navigateCommit(1); cmd != nil {
			return m, cmd
		}
	case key.Matches(msg, keys.PrevCommit):
		if cmd := m.navigateCommit(-1); cmd != nil {
			return m, cmd
		}

	case key.Matches(msg, keys.VisualMode):
		m.mode = modeVisual
		m.diffView.selectionAnchor = m.diffView.cursorY
		m.diffView.selectionActive = true
		return m, nil

	case key.Matches(msg, keys.Comment):
		file, ok := m.currentReviewFileName()
		if !ok {
			break
		}
		lineNum := m.diffView.currentLineNum()
		if lineNum == 0 {
			break // can't comment on hunk headers
		}
		m.mode = modeComment
		m.diffView.activateComment(file, lineNum)
		return m, m.diffView.commentInput.Focus()

	case key.Matches(msg, keys.ViewGeneral):
		m.mode = modeViewGeneral
		m.generalViewIdx = 0
		return m, nil

	case key.Matches(msg, keys.GeneralComment):
		m.mode = modeGeneralComment
		m.generalEditIdx = -1
		m.generalInput = m.newGeneralTextarea("")
		return m, m.generalInput.Focus()

	case key.Matches(msg, keys.DeleteComment):
		file, ok := m.currentReviewFileName()
		if !ok {
			break
		}
		lineNum := m.diffView.currentLineNum()
		if c := m.diffView.commentAtCursor(); c != nil {
			m.review.DeleteCommentRange(c.File, c.Line, c.EndLine)
			m.dirty = true
			m.diffView.buildRows()
		} else if m.review.FindComment(file, lineNum) != nil {
			m.review.DeleteComment(file, lineNum)
			m.dirty = true
			m.diffView.buildRows()
		}

	case key.Matches(msg, keys.EditComment):
		file, ok := m.currentReviewFileName()
		if !ok {
			break
		}
		if c := m.diffView.commentAtCursor(); c != nil {
			m.mode = modeComment
			m.diffView.activateEditRangeComment(file, c.Line, c.EndLine, c.Text)
			return m, m.diffView.commentInput.Focus()
		}
		lineNum := m.diffView.currentLineNum()
		if c := m.review.FindComment(file, lineNum); c != nil {
			m.mode = modeComment
			m.diffView.activateEditComment(file, lineNum, c.Text)
			return m, m.diffView.commentInput.Focus()
		}

	case key.Matches(msg, keys.Search):
		m.mode = modeSearch
		m.bottomBar.activate("/")
		return m, m.bottomBar.input.Focus()
	case key.Matches(msg, keys.FileSearch):
		m.mode = modeFileSearch
		m.bottomBar.activate("Find file:")
		return m, m.bottomBar.input.Focus()
	case key.Matches(msg, keys.ContentSearch):
		m.mode = modeContentSearch
		m.bottomBar.activate("Find content:")
		return m, m.bottomBar.input.Focus()

	case key.Matches(msg, keys.SearchNext):
		for i := 0; i < count; i++ {
			m.jumpToNextMatch(1)
		}
	case key.Matches(msg, keys.SearchPrev):
		for i := 0; i < count; i++ {
			m.jumpToNextMatch(-1)
		}

	case key.Matches(msg, keys.Expand):
		_, idx, ok := m.fileList.modifiedSelection()
		if !ok {
			break
		}
		m.expandedSet[idx] = !m.expandedSet[idx]
		if m.expandedSet[idx] {
			if m.expandedFiles != nil {
				m.setDiffViewForSelection(true)
			} else {
				return m, m.loadExpandedDiff()
			}
		} else {
			m.setDiffViewForSelection(true)
		}

	case key.Matches(msg, keys.ToggleRelativeNum):
		m.diffView.cycleLineNumMode()

	case key.Matches(msg, keys.ToggleView):
		m.diffView.toggleMode()

	case key.Matches(msg, keys.Help):
		m.mode = modeHelp

	case key.Matches(msg, keys.Save):
		return m.beginSaveAndQuit()
	}

	return m, nil
}

func (m Model) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
	headerH := lipgloss.Height(m.renderHeader())
	// Body starts at y == headerH. Panels have a 1-line border at top.
	bodyY := y - headerH - 1 // row index within panel content area

	if bodyY < 0 || bodyY >= m.diffView.height {
		m.mouseDrag.active = false
		return m, nil
	}

	// File list panel occupies columns 0..fileListWidth+1 (including border)
	if x <= m.fileListWidth+1 {
		m.mouseDrag.active = false
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		if m.fileList.clickAt(bodyY) {
			m.setDiffViewForSelection(false)
		}
		return m, nil
	}

	// Diff panel: content starts 1 line below border top (path line)
	diffY := bodyY - 1 // subtract path line
	if diffY >= 0 {
		row := m.diffView.scrollY + diffY
		if row >= 0 && row < len(m.diffView.rows) {
			m.mouseDrag.active = true
			m.mouseDrag.startRow = row
		}
		m.diffView.clickAt(diffY)
	}
	return m, nil
}

// handleMouseDrag extends a visual selection as the user drags in the diff panel.
func (m Model) handleMouseDrag(x, y int) (tea.Model, tea.Cmd) {
	if !m.mouseDrag.active {
		return m, nil
	}

	headerH := lipgloss.Height(m.renderHeader())
	bodyY := y - headerH - 1
	if bodyY < 0 || bodyY >= m.diffView.height {
		return m, nil
	}

	// Only drag in the diff panel
	if x <= m.fileListWidth+1 {
		return m, nil
	}

	diffY := bodyY - 1
	if diffY < 0 {
		return m, nil
	}

	row := m.diffView.scrollY + diffY
	if row < 0 {
		row = 0
	}
	if row >= len(m.diffView.rows) {
		row = len(m.diffView.rows) - 1
	}

	// Don't enter visual mode until the mouse actually moves to a different row
	if row == m.mouseDrag.startRow {
		return m, nil
	}

	if m.mode != modeVisual {
		m.mode = modeVisual
		m.diffView.selectionAnchor = m.mouseDrag.startRow
		m.diffView.selectionActive = true
	}
	m.diffView.cursorY = row
	return m, nil
}

// consumeCount reads and clears the count prefix buffer, returning at least 1.
func (m *Model) consumeCount() int {
	if m.countBuf == "" {
		return 1
	}
	n, err := strconv.Atoi(m.countBuf)
	m.countBuf = ""
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func (m Model) updateVisual(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = ""
	k := msg.String()

	// Handle 'gg' sequence
	if m.pendingG {
		m.pendingG = false
		if k == "g" {
			count := m.consumeCount()
			if count > 1 {
				m.diffView.moveCursorTo(count - 1)
			} else {
				m.diffView.moveCursorTo(0)
			}
			return m, nil
		}
		m.countBuf = ""
	}

	// Accumulate digit prefix
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && m.countBuf == "" {
		m.countBuf = k
		return m, nil
	}
	if len(k) == 1 && k[0] >= '0' && k[0] <= '9' && m.countBuf != "" {
		m.countBuf += k
		return m, nil
	}

	if k == "g" {
		m.pendingG = true
		return m, nil
	}

	count := m.consumeCount()
	if k == "G" {
		if count > 1 {
			m.diffView.moveCursorTo(count - 1)
		} else {
			m.diffView.moveCursorTo(len(m.diffView.rows) - 1)
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.Cancel) || key.Matches(msg, keys.VisualMode):
		m.mode = modeNormal
		m.diffView.selectionActive = false
		return m, nil

	case key.Matches(msg, keys.Down):
		m.diffView.moveCursor(count)
	case key.Matches(msg, keys.Up):
		m.diffView.moveCursor(-count)
	case key.Matches(msg, keys.ScreenTop):
		m.diffView.moveCursorToViewportTop()
	case key.Matches(msg, keys.ScreenBottom):
		m.diffView.moveCursorToViewportBottom()
	case key.Matches(msg, keys.FullPageDown):
		m.diffView.moveCursor(count * m.diffView.contentViewportHeight())
	case key.Matches(msg, keys.FullPageUp):
		m.diffView.moveCursor(-count * m.diffView.contentViewportHeight())
	case key.Matches(msg, keys.HalfPageDown):
		m.diffView.moveCursor(count * m.diffView.height / 2)
	case key.Matches(msg, keys.HalfPageUp):
		m.diffView.moveCursor(-count * m.diffView.height / 2)

	case key.Matches(msg, keys.Comment):
		file, ok := m.currentReviewFileName()
		if !ok {
			break
		}
		startLine, endLine := m.diffView.selectionLineRange()
		if startLine == 0 {
			break
		}
		m.mode = modeComment
		if startLine == endLine {
			m.diffView.selectionActive = false
			m.diffView.activateComment(file, startLine)
		} else {
			// Keep selection highlight visible while composing the range comment
			m.diffView.activateRangeComment(file, startLine, endLine)
		}
		return m, m.diffView.commentInput.Focus()
	}

	return m, nil
}

func (m Model) updateComment(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.diffView.deactivateComment()
		return m, nil

	case key.Matches(msg, keys.OpenEditor):
		// Open $EDITOR with current comment text
		tmpFile, err := os.CreateTemp("", "cr-comment-*.txt")
		if err != nil {
			m.err = err
			return m, nil
		}
		currentText := m.diffView.commentValue()
		if currentText != "" {
			tmpFile.WriteString(currentText)
		}
		tmpFile.Close()
		editor := editorCmd()
		c := exec.Command(editor, tmpFile.Name())
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return editorFinishedMsg{tmpFile: tmpFile.Name(), err: err}
		})

	case key.Matches(msg, keys.SubmitComment):
		text := strings.TrimSpace(m.diffView.commentValue())
		if text != "" {
			if m.diffView.commentEditing {
				m.review.DeleteCommentRange(m.diffView.commentFile, m.diffView.commentLineNum, m.diffView.commentEndLine)
			}
			if m.diffView.commentEndLine > 0 {
				m.review.AddRangeComment(m.diffView.commentFile, m.diffView.commentLineNum, m.diffView.commentEndLine, text)
			} else {
				m.review.AddComment(m.diffView.commentFile, m.diffView.commentLineNum, text)
			}
			m.dirty = true
			m.diffView.deactivateComment()
			m.diffView.buildRows()
		} else {
			m.diffView.deactivateComment()
		}
		m.mode = modeNormal
		return m, nil
	}

	// Forward to textarea (enter inserts newlines)
	var cmd tea.Cmd
	m.diffView.commentInput, cmd = m.diffView.commentInput.Update(msg)
	return m, cmd
}

// generalEditorFinishedMsg is sent when $EDITOR exits for a general comment.
type generalEditorFinishedMsg struct {
	tmpFile string
	err     error
}

func (m Model) updateGeneralComment(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		return m, nil

	case key.Matches(msg, keys.OpenEditor):
		tmpFile, err := os.CreateTemp("", "cr-general-*.txt")
		if err != nil {
			m.err = err
			return m, nil
		}
		currentText := m.generalInput.Value()
		if currentText != "" {
			tmpFile.WriteString(currentText)
		}
		tmpFile.Close()
		editor := editorCmd()
		c := exec.Command(editor, tmpFile.Name())
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return generalEditorFinishedMsg{tmpFile: tmpFile.Name(), err: err}
		})

	case key.Matches(msg, keys.SubmitComment):
		text := strings.TrimSpace(m.generalInput.Value())
		if text != "" {
			if m.generalEditIdx >= 0 {
				m.review.EditGeneralComment(m.generalEditIdx, text)
				m.statusMsg = "General comment updated"
			} else {
				m.review.AddGeneralComment(text)
				m.statusMsg = fmt.Sprintf("General comment added (%d total)", len(m.review.GeneralComments))
			}
			m.dirty = true
		}
		m.mode = modeNormal
		return m, nil
	}

	var cmd tea.Cmd
	m.generalInput, cmd = m.generalInput.Update(msg)
	return m, cmd
}

func (m *Model) newGeneralTextarea(initial string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Enter comment... (enter=newline, ctrl+enter=submit, ctrl+g=editor)"
	ta.CharLimit = 2000
	w := m.width - 20
	if w < 40 {
		w = 40
	}
	ta.SetWidth(w)
	ta.SetHeight(5)
	if initial != "" {
		ta.SetValue(initial)
	}
	return ta
}

func (m Model) updateViewGeneral(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	total := len(m.review.GeneralComments)
	switch {
	case key.Matches(msg, keys.Cancel) || k == "q":
		m.mode = modeNormal
		return m, nil
	case k == "j" || k == "down":
		if m.generalViewIdx < total-1 {
			m.generalViewIdx++
		}
	case k == "k" || k == "up":
		if m.generalViewIdx > 0 {
			m.generalViewIdx--
		}
	case k == "d":
		if total > 0 {
			m.review.DeleteGeneralComment(m.generalViewIdx)
			m.dirty = true
			m.statusMsg = "General comment deleted"
			total = len(m.review.GeneralComments)
			if m.generalViewIdx >= total && total > 0 {
				m.generalViewIdx = total - 1
			}
		}
	case k == "E":
		if total > 0 {
			m.mode = modeGeneralComment
			m.generalEditIdx = m.generalViewIdx
			m.generalInput = m.newGeneralTextarea(m.review.GeneralComments[m.generalViewIdx])
			return m, m.generalInput.Focus()
		}
	case k == "R":
		m.mode = modeGeneralComment
		m.generalEditIdx = -1
		m.generalInput = m.newGeneralTextarea("")
		return m, m.generalInput.Focus()
	}
	return m, nil
}

func (m Model) viewGeneralComments() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPurple).
		MarginBottom(1).
		Render("General Comments")

	var rows []string
	if len(m.review.GeneralComments) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colorDim).Render("No general comments yet. Press R to add one."))
	} else {
		for i, c := range m.review.GeneralComments {
			prefix := fmt.Sprintf("%d. ", i+1)
			// Truncate long comments for display
			display := strings.ReplaceAll(c, "\n", " ↵ ")
			maxW := m.width - 30
			if maxW < 20 {
				maxW = 20
			}
			if lipgloss.Width(display) > maxW {
				display = truncateToWidth(display, maxW)
			}
			line := prefix + display
			if i == m.generalViewIdx {
				rows = append(rows, cursorStyle.Render(line))
			} else {
				rows = append(rows, lipgloss.NewStyle().Foreground(colorFg).Render(line))
			}
		}
	}

	hint := lipgloss.NewStyle().Foreground(colorDim).MarginTop(1).Render(
		"j/k navigate  d delete  E edit  R add  Esc close")

	content := title + "\n" + strings.Join(rows, "\n") + "\n" + hint

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPurple).
		Padding(1, 3).
		Render(content)

	return lipgloss.Place(m.width, m.height-2, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewGeneralCommentInput() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorYellow).
		MarginBottom(1).
		Render("General Comment")

	hint := lipgloss.NewStyle().Foreground(colorDim).MarginTop(1).Render(
		"ctrl+enter submit | ctrl+g editor | esc cancel")

	content := title + "\n" + m.generalInput.View() + "\n" + hint

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Padding(1, 3).
		Render(content)

	return lipgloss.Place(m.width, m.height-2, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m, nil

	case key.Matches(msg, keys.Submit):
		term := m.bottomBar.value()
		m.mode = modeNormal
		m.bottomBar.deactivate()
		if term != "" {
			m.searchTerm = term
			m.searchMatches = m.diffView.findMatches(term)
			m.searchIdx = 0
			// Jump to first match at or after cursor
			m.jumpToNextMatch(1)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.bottomBar.input, cmd = m.bottomBar.input.Update(msg)
	return m, cmd
}

func (m Model) updateFileSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m, nil

	case key.Matches(msg, keys.Submit):
		term := m.bottomBar.value()
		m.mode = modeNormal
		m.bottomBar.deactivate()
		if term != "" {
			m.fileStates[m.currentStateKey()] = m.diffView.saveState()
			found, err := m.fileList.search(term)
			if err != nil {
				m.err = err
				return m, nil
			}
			if found {
				m.setDiffViewForSelection(false)
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.bottomBar.input, cmd = m.bottomBar.input.Update(msg)
	return m, cmd
}

func (m Model) updateContentSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m, nil

	case key.Matches(msg, keys.Submit):
		term := m.bottomBar.value()
		m.mode = modeNormal
		m.bottomBar.deactivate()
		if term != "" {
			m.fileStates[m.currentStateKey()] = m.diffView.saveState()
			path, found, err := m.fileList.searchContent(term)
			if err != nil {
				m.err = err
				return m, nil
			}
			if found {
				if err := m.fileList.focusPath(path); err != nil {
					m.err = err
					return m, nil
				}
				m.setDiffViewForSelection(false)
				if matches := m.diffView.findMatches(term); len(matches) > 0 {
					m.diffView.moveCursorTo(matches[0])
				}
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.bottomBar.input, cmd = m.bottomBar.input.Update(msg)
	return m, cmd
}

func (m Model) updateSaveAs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m, nil
	case key.Matches(msg, keys.Submit):
		filename := strings.TrimSpace(m.bottomBar.value())
		if filename == "" {
			filename = defaultReviewOutputFile
		}
		m.config.OutputFile = filename
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m.doSaveAndQuit()
	}
	var cmd tea.Cmd
	m.bottomBar.input, cmd = m.bottomBar.input.Update(msg)
	return m, cmd
}

func (m Model) updateQuitConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := strings.ToLower(msg.String())
	switch {
	case key.Matches(msg, keys.Cancel) || k == "c":
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m, nil
	case k == "y":
		m.mode = modeNormal
		m.bottomBar.deactivate()
		return m.beginSaveAndQuit()
	case k == "n" || key.Matches(msg, keys.Quit):
		m.mode = modeNormal
		m.bottomBar.deactivate()
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) hasUnsavedReview() bool {
	return m.dirty
}

func (m Model) beginSaveAndQuit() (tea.Model, tea.Cmd) {
	if m.config.PromptSaveAs && m.config.OutputFile == "" {
		m.mode = modeSaveAs
		m.bottomBar.activate("Save review as:")
		m.bottomBar.input.SetValue(defaultReviewOutputFile)
		m.bottomBar.input.SetCursor(len(defaultReviewOutputFile))
		return m, m.bottomBar.input.Focus()
	}
	return m.doSaveAndQuit()
}

func (m Model) doSaveAndQuit() (tea.Model, tea.Cmd) {
	m.saving = true
	m.dirty = false
	return m, tea.Quit
}

// jumpToNextMatch moves the cursor to the next (dir=1) or previous (dir=-1) search match.
func (m *Model) jumpToNextMatch(dir int) {
	if len(m.searchMatches) == 0 {
		return
	}
	cursor := m.diffView.cursorY
	if dir > 0 {
		// Find next match after cursor
		for _, idx := range m.searchMatches {
			if idx > cursor {
				m.diffView.moveCursorTo(idx)
				return
			}
		}
		// Wrap around
		m.diffView.moveCursorTo(m.searchMatches[0])
	} else {
		// Find previous match before cursor
		for i := len(m.searchMatches) - 1; i >= 0; i-- {
			if m.searchMatches[i] < cursor {
				m.diffView.moveCursorTo(m.searchMatches[i])
				return
			}
		}
		// Wrap around
		m.diffView.moveCursorTo(m.searchMatches[len(m.searchMatches)-1])
	}
}

func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Help), key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Quit):
		m.mode = modeNormal
	}
	return m, nil
}

func (m Model) helpView() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPurple).
		MarginBottom(1).
		Render("Keybindings")

	entries := []struct{ key, desc string }{
		{"j / k / ↑ / ↓", "Move cursor up/down"},
		{"[count]j/k", "Move by count (e.g. 9j)"},
		{"H / L", "Top / bottom visible line"},
		{"gg / G", "Go to top / bottom"},
		{"[count]gg / [count]G", "Go to line"},
		{"PgDn / PgUp / Ctrl+f / Ctrl+b", "Page down/up (viewport height)"},
		{"Ctrl+d / Ctrl+u", "Half page down/up"},
		{"/", "Search in diff"},
		{"f", "Find file by name/path"},
		{"p", "Find file by content"},
		{"n / N", "Next / previous match"},
		{"] / [ / → / ←", "Next / previous file"},
		{"} / {", "Next / previous modified file"},
		{"M", "Jump to first modified file"},
		{"t", "Toggle changed / all files"},
		{"o", "Open/close selected directory"},
		{"< / >", "Shrink / grow file panel"},
		{"h / l", "Previous / next commit"},
		{"V", "Visual select (then c to comment)"},
		{"c", "Add inline comment at cursor"},
		{"R", "Add general comment (multi-line)"},
		{"Ctrl+r", "View/manage general comments"},
		{"Ctrl+Enter", "Submit comment"},
		{"Enter", "Newline in comment"},
		{"Ctrl+g", "Open $EDITOR for comment"},
		{"Esc", "Cancel comment"},
		{"d", "Delete comment at cursor"},
		{"E", "Edit comment at cursor"},
		{"Tab", "Toggle side-by-side / unified"},
		{"Ctrl+n", "Cycle line numbers (both/rel/abs)"},
		{"e", "Toggle full file context"},
		{"s", "Save review and exit"},
		{"q / Ctrl+c", "Quit (prompts if unsaved)"},
		{"Shift+click", "Context menu"},
		{"?", "Toggle this help screen"},
	}

	keyStyle := lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Width(20)
	descStyle := lipgloss.NewStyle().Foreground(colorFg)

	var rows []string
	for _, e := range entries {
		row := keyStyle.Render(e.key) + descStyle.Render(e.desc)
		rows = append(rows, row)
	}

	content := title + "\n" + strings.Join(rows, "\n") + "\n\n" +
		lipgloss.NewStyle().Foreground(colorDim).Render("Press ? or Esc to close")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPurple).
		Padding(1, 3).
		Render(content)

	return lipgloss.Place(m.width, m.height-2, lipgloss.Center, lipgloss.Center, box)
}

// commitLoadedMsg is sent when async commit navigation finishes.
type commitLoadedMsg struct {
	files  []diff.FileDiff
	commit gitpkg.CommitInfo
	idx    int
	err    error
}

func (m *Model) navigateCommit(delta int) tea.Cmd {
	newIdx := m.commitIdx + delta
	if newIdx < 0 || newIdx >= len(m.commits) {
		return nil
	}
	m.commitIdx = newIdx
	m.expandedSet = make(map[int]bool)
	m.expandedFiles = nil
	m.fileStates = make(map[string]fileState)
	m.referenceFiles = make(map[string]*diff.FileDiff)
	commit := m.commits[newIdx]
	m.config.RevSpec = commit.Hash
	m.review.CommitHash = commit.Hash
	m.review.CommitSubject = commit.Subject

	return func() tea.Msg {
		rawDiff, err := m.git.Diff(commit.Hash)
		if err != nil {
			return commitLoadedMsg{err: err, idx: newIdx, commit: commit}
		}
		files, err := diff.Parse(rawDiff)
		if err != nil {
			return commitLoadedMsg{err: err, idx: newIdx, commit: commit}
		}
		return commitLoadedMsg{files: files, idx: newIdx, commit: commit}
	}
}

func (m *Model) updateLayout() {
	m.updateLayoutWithChrome(0)
}

// updateLayoutWithChrome computes panel sizes given extra chrome lines
// (e.g. bottom bar) beyond header/footer/borders.
func (m *Model) updateLayoutWithChrome(extraLines int) {
	// Measure actual header + footer heights to handle text wrapping on
	// narrow terminals. Panel borders add 2 lines (top + bottom).
	headerH := lipgloss.Height(m.renderHeader())
	footerH := lipgloss.Height(m.renderFooter())
	const panelBorderH = 2
	contentHeight := m.height - headerH - footerH - panelBorderH - extraLines
	if contentHeight < minContentHeight {
		contentHeight = minContentHeight
	}
	m.fileList.height = contentHeight
	m.diffView.width = m.width - m.fileListWidth - 2
	m.diffView.height = contentHeight
	m.diffView.clampScroll()
}

func (m Model) renderHeader() string {
	headerText := " cr"
	if len(m.commits) > 0 && m.commitIdx < len(m.commits) {
		c := m.commits[m.commitIdx]
		headerText = fmt.Sprintf(" [%s] %s", c.Hash, c.Subject)
	} else if strings.Contains(m.config.RevSpec, "...") {
		headerText = fmt.Sprintf(" [branch diff] %s", m.config.RevSpec)
	} else if m.config.RevSpec == "" {
		headerText = " [working tree] uncommitted changes"
	} else {
		headerText = fmt.Sprintf(" [diff] %s", m.config.RevSpec)
	}
	w := m.width
	if w < 3 {
		w = 3
	}
	return headerStyle.Width(w).Height(1).Render(truncateToWidth(headerText, w-2))
}

func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxWidth-1 {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "…"
	}
	return string(runes) + "…"
}

func (m Model) renderFooter() string {
	modeStr := "side-by-side"
	if m.diffView.mode == viewUnified {
		modeStr = "unified"
	}
	filePos, fileTotal := m.fileList.counts()
	fileCount := fmt.Sprintf("%d/%d files", filePos, fileTotal)
	fileMode := "changed"
	if m.fileList.mode == fileListModeFullTree {
		fileMode = "all"
	}
	commentParts := []string{fmt.Sprintf("%d comments", len(m.review.Comments))}
	if gc := len(m.review.GeneralComments); gc > 0 {
		commentParts = append(commentParts, fmt.Sprintf("%d general", gc))
	}
	commentCount := strings.Join(commentParts, ", ")
	footerHints := fmt.Sprintf(" `j/k`move `H/L`screen-top/bot `gg/G`top/bot `PgDn/Up`page `/`search `n/N`next/prev `V`visual `c`comment `R`general `^r`view `d/E`del/edit `tab`%s `e`expand `s`save `q`quit `?`help",
		modeStr)
	if m.mode == modeVisual {
		footerHints = " -- VISUAL -- j/k move  c comment on selection  Esc cancel"
	}
	footerPrimary := footerStyle.MaxWidth(m.width).Render(footerHints)
	dividerWidth := m.width
	if dividerWidth < 1 {
		dividerWidth = 1
	}
	footerDivider := footerStyle.MaxWidth(m.width).Render(strings.Repeat("─", dividerWidth))
	navInfo := fmt.Sprintf(" `f`find `p`content `]/[/←/→`next/prev `}/{`modified `M`first `t`all-files `o`dir `</>`resize  [%s] %s  %s",
		fileMode, fileCount, commentCount)
	if m.statusMsg != "" {
		navInfo += "  " + lipgloss.NewStyle().Foreground(colorYellow).Render(m.statusMsg)
	}
	footerNav := footerStyle.MaxWidth(m.width).Render(navInfo)
	return lipgloss.JoinVertical(lipgloss.Left, footerPrimary, footerDivider, footerNav)
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}
	if m.quitting {
		return ""
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	// Help overlay
	if m.mode == modeHelp {
		return lipgloss.JoinVertical(lipgloss.Left, header, m.helpView(), footer)
	}

	// General comments viewer overlay
	if m.mode == modeViewGeneral {
		return lipgloss.JoinVertical(lipgloss.Left, header, m.viewGeneralComments(), footer)
	}

	// General comment input overlay
	if m.mode == modeGeneralComment {
		return lipgloss.JoinVertical(lipgloss.Left, header, m.viewGeneralCommentInput(), footer)
	}

	// Recompute panel heights to match actual chrome (handles footer wrapping)
	hasBottomBar := m.mode == modeSearch || m.mode == modeFileSearch || m.mode == modeContentSearch || m.mode == modeSaveAs || m.mode == modeQuitConfirm
	extraLines := 0
	if hasBottomBar {
		extraLines = lipgloss.Height(m.bottomBar.view())
	}
	m.updateLayoutWithChrome(extraLines)

	// Body
	fl := m.fileList.view(m.fileListWidth)
	dv := m.diffView.viewWithPath(m.fileList.selectedDiffPath())
	body := lipgloss.JoinHorizontal(lipgloss.Top, fl, dv)

	var result string
	if hasBottomBar {
		result = lipgloss.JoinVertical(lipgloss.Left, header, body, m.bottomBar.view(), footer)
	} else {
		result = lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	}

	if m.mode == modeContextMenu {
		result = overlayAt(result, m.renderContextMenu(), m.ctxMenu.x, m.ctxMenu.y)
	}

	return result
}

// GetReview returns the review data (used after quit).
func (m Model) GetReview() *review.Review {
	return m.review
}

// IsSaving returns whether the user triggered a save.
func (m Model) IsSaving() bool {
	return m.saving
}

// GetOutputFile returns the selected output filename (if any).
func (m Model) GetOutputFile() string {
	return m.config.OutputFile
}

// GetFilesForOutput returns the currently loaded file diffs used in the review session.
func (m Model) GetFilesForOutput() []diff.FileDiff {
	out := make([]diff.FileDiff, len(m.fileList.files))
	copy(out, m.fileList.files)
	return out
}
