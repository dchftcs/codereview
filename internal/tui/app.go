package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	modeVisual
	modeContextMenu
)

type focusTarget int

const (
	focusDiff focusTarget = iota
	focusGeneralPanel
)

// GitService is the narrow git surface needed by the TUI.
type GitService interface {
	Diff(revSpec string) (string, []gitpkg.CollapsedDir, error)
	DiffFull(revSpec string) (string, []gitpkg.CollapsedDir, error)
	DiffUnstaged() (string, []gitpkg.CollapsedDir, error)
	DiffUnstagedFull() (string, []gitpkg.CollapsedDir, error)
	Log(n int) ([]gitpkg.CommitInfo, error)
	Status() ([]gitpkg.FileStatus, error)
	Stage(path string) error
	Unstage(path string) error
	UntrackedFiles() ([]string, error)
}

type defaultGitService struct{}

func (defaultGitService) Diff(revSpec string) (string, []gitpkg.CollapsedDir, error) {
	return gitpkg.Diff(revSpec)
}

func (defaultGitService) DiffFull(revSpec string) (string, []gitpkg.CollapsedDir, error) {
	return gitpkg.DiffFull(revSpec)
}

func (defaultGitService) DiffUnstaged() (string, []gitpkg.CollapsedDir, error) {
	return gitpkg.DiffUnstaged()
}

func (defaultGitService) DiffUnstagedFull() (string, []gitpkg.CollapsedDir, error) {
	return gitpkg.DiffUnstagedFull()
}

func (defaultGitService) Log(n int) ([]gitpkg.CommitInfo, error) {
	return gitpkg.Log(n)
}

func (defaultGitService) Status() ([]gitpkg.FileStatus, error) {
	return gitpkg.Status()
}

func (defaultGitService) Stage(path string) error {
	return gitpkg.Stage(path)
}

func (defaultGitService) Unstage(path string) error {
	return gitpkg.Unstage(path)
}

func (defaultGitService) UntrackedFiles() ([]string, error) {
	return gitpkg.UntrackedFiles()
}

// Config holds TUI startup configuration.
type Config struct {
	RevSpec      string
	PathFilter   string
	UnstagedOnly bool
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
const generalCommentInputContentHeight = 7
const mouseDoubleClickThreshold = 350 * time.Millisecond
const idleStatusPollInterval = 300 * time.Millisecond
const activeStatusPollDebounce = 1 * time.Second

type statusSnapshotEntry struct {
	Index     byte
	Worktree  byte
	Staged    bool
	Untracked bool
}

type Model struct {
	config         Config
	mode           mode
	width          int
	height         int
	fileListWidth  int
	fileList       fileList
	diffView       diffView
	focus          focusTarget
	generalPanel   generalPanel
	bottomBar      bottomBarInput
	review         *review.Review
	commits        []gitpkg.CommitInfo
	git            GitService
	commitIdx      int
	expandedSet    map[int]bool    // per-file expand tracking
	expandedFiles  []diff.FileDiff // cached full-context diffs
	fileStates     map[string]fileState
	referenceFiles map[string]*diff.FileDiff
	// General comment editor
	generalInput   textarea.Model // multi-line textarea for general comments
	generalEditIdx int            // -1 for new comment, >=0 for editing existing
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
	// Double-click tracking for the general panel.
	lastGeneralClick struct {
		at     time.Time
		panelY int
	}
	// Double-click tracking for diff comment rows.
	lastDiffClick struct {
		at  time.Time
		key string
	}
	statusSnapshot     map[string]statusSnapshotEntry
	statusEpoch        int
	statusAppliedEpoch int
	statusPollInFlight bool
	gitOpInFlight      bool // true while a stage toggle or diff reload goroutine is running
	lastUserAction     time.Time
	now                func() time.Time
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
		focus:          focusDiff,
		generalPanel:   generalPanel{},
		bottomBar:      newBottomBarInput(),
		git:            cfg.Git,
		expandedSet:    make(map[int]bool),
		fileStates:     make(map[string]fileState),
		referenceFiles: make(map[string]*diff.FileDiff),
		statusSnapshot: make(map[string]statusSnapshotEntry),
		statusEpoch:    1,
		now:            time.Now,
	}
	applyDiffContext(m.review, cfg.RevSpec, cfg.UnstagedOnly)
	m.generalPanel.review = m.review
	return m
}

func applyDiffContext(rev *review.Review, revSpec string, unstagedOnly bool) {
	rev.DiffLeft = ""
	rev.DiffRight = ""
	rev.IncludesStaged = false
	rev.IncludesUnstaged = false
	rev.IncludesUntracked = false

	if unstagedOnly {
		rev.DiffLeft = "index"
		rev.DiffRight = "working tree"
		rev.IncludesUnstaged = true
		rev.IncludesUntracked = true
		return
	}

	if revSpec == "" {
		rev.DiffLeft = "HEAD"
		rev.DiffRight = "working tree"
		rev.IncludesStaged = true
		rev.IncludesUnstaged = true
		rev.IncludesUntracked = true
		return
	}

	if strings.Contains(revSpec, "...") {
		parts := strings.SplitN(revSpec, "...", 2)
		base, head := parts[0], parts[1]
		rev.DiffLeft = fmt.Sprintf("merge-base(%s,%s)", base, head)
		rev.DiffRight = head
		if head == "HEAD" {
			rev.IncludesStaged = true
			rev.IncludesUnstaged = true
			rev.IncludesUntracked = true
		}
		return
	}

	if strings.Contains(revSpec, "..") {
		parts := strings.SplitN(revSpec, "..", 2)
		rev.DiffLeft = parts[0]
		rev.DiffRight = parts[1]
		return
	}

	// Single-commit mode (`git show <commit>`): parent on LHS, commit on RHS.
	rev.DiffLeft = revSpec + "^"
	rev.DiffRight = revSpec
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadDiffForStatusEpoch("", m.statusEpoch), scheduleStatusPoll())
}

type diffLoadedMsg struct {
	files             []diff.FileDiff
	commits           []gitpkg.CommitInfo
	untracked         []string
	collapsedDirs     []gitpkg.CollapsedDir
	statuses          []gitpkg.FileStatus
	preserveSelection string
	statusEpoch       int
	err               error
}

func (m *Model) loadDiff() tea.Cmd {
	return m.loadDiffWithSelection("")
}

func (m *Model) loadDiffWithSelection(preserveSelection string) tea.Cmd {
	epoch := m.nextStatusEpoch()
	return m.loadDiffForStatusEpoch(preserveSelection, epoch)
}

func (m Model) loadDiffForStatusEpoch(preserveSelection string, statusEpoch int) tea.Cmd {
	return func() tea.Msg {
		var (
			rawDiff      string
			collapsedDirs []gitpkg.CollapsedDir
			err          error
		)
		if m.config.UnstagedOnly {
			rawDiff, collapsedDirs, err = m.git.DiffUnstaged()
		} else {
			rawDiff, collapsedDirs, err = m.git.Diff(m.config.RevSpec)
		}
		if err != nil {
			return diffLoadedMsg{err: err}
		}

		files, err := diff.Parse(rawDiff)
		if err != nil {
			return diffLoadedMsg{err: err}
		}

		commits, _ := m.git.Log(50)
		statuses, _ := m.git.Status()
		untracked, _ := m.git.UntrackedFiles()

		return diffLoadedMsg{
			files:             files,
			commits:           commits,
			untracked:         untracked,
			collapsedDirs:     collapsedDirs,
			statuses:          statuses,
			preserveSelection: preserveSelection,
			statusEpoch:       statusEpoch,
		}
	}
}

type stageToggledMsg struct {
	action            string
	path              string
	staged            bool
	preserveSelection string
	err               error
}

type pollTickMsg struct{}

type statusPolledMsg struct {
	snapshot map[string]statusSnapshotEntry
	epoch    int
	err      error
}

func scheduleStatusPoll() tea.Cmd {
	return tea.Tick(idleStatusPollInterval, func(time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

func snapshotFromGitStatuses(statuses []gitpkg.FileStatus) map[string]statusSnapshotEntry {
	snapshot := make(map[string]statusSnapshotEntry, len(statuses))
	for _, st := range statuses {
		path := filepath.ToSlash(st.Path)
		snapshot[path] = statusSnapshotEntry{
			Index:     st.Index,
			Worktree:  st.Worktree,
			Staged:    st.Index != ' ' && st.Index != '?',
			Untracked: st.Index == '?' && st.Worktree == '?',
		}
	}
	return snapshot
}

func shouldIncludeInCurrentReview(snapshot statusSnapshotEntry, cfg Config) bool {
	if cfg.UnstagedOnly {
		return snapshot.Untracked || (snapshot.Worktree != ' ' && snapshot.Worktree != 0)
	}
	if cfg.RevSpec == "" || strings.HasSuffix(cfg.RevSpec, "...HEAD") {
		return snapshot.Untracked || snapshot.Index != ' ' || snapshot.Worktree != ' '
	}
	return false
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
		var (
			rawDiff string
			err     error
		)
		if m.config.UnstagedOnly {
			rawDiff, _, err = m.git.DiffUnstagedFull()
		} else {
			rawDiff, _, err = m.git.DiffFull(m.config.RevSpec)
		}
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

	case pollTickMsg:
		if !m.canToggleStage() || m.statusPollInFlight || m.gitOpInFlight || m.now().Sub(m.lastUserAction) < activeStatusPollDebounce {
			return m, scheduleStatusPoll()
		}
		m.statusPollInFlight = true
		epoch := m.nextStatusEpoch()
		return m, tea.Batch(scheduleStatusPoll(), m.pollStatus(epoch))

	case diffLoadedMsg:
		m.gitOpInFlight = false
		m.applyDiffLoaded(msg)
		return m, nil

	case commitLoadedMsg:
		m.applyCommitLoaded(msg)
		return m, nil

	case expandLoadedMsg:
		m.applyExpandLoaded(msg)
		return m, nil

	case stageToggledMsg:
		if msg.err != nil {
			m.gitOpInFlight = false
			m.err = msg.err
			return m, nil
		}
		m.applyOptimisticStageToggle(msg.path, msg.staged)
		m.statusMsg = msg.action
		// gitOpInFlight stays true — cleared when diffLoadedMsg arrives
		return m, m.loadDiffWithSelection(msg.preserveSelection)

	case statusPolledMsg:
		m.statusPollInFlight = false
		if msg.err != nil {
			return m, nil
		}
		prevSnapshot := m.statusSnapshot
		if !m.applyStatusSnapshot(msg.snapshot, msg.epoch) {
			return m, nil
		}
		if shouldReloadForStatusTransition(prevSnapshot, msg.snapshot, m.config) {
			m.gitOpInFlight = true
			return m, m.loadDiffWithSelection(m.fileList.selectedDiffPath())
		}
		m.applySnapshotToVisibleFiles()
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
			m.review.AddGeneralComment(text)
			m.statusMsg = "General comment saved"
			m.dirty = true
			m.syncGeneralPanelAfterSubmit()
			m.updateLayout()
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
		m.lastUserAction = m.now()
		if m.mode == modeContextMenu {
			return m.updateContextMenuMouse(msg)
		}
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelDown:
				return m.handleMouseWheel(msg.X, msg.Y, 3)
			case tea.MouseButtonWheelUp:
				return m.handleMouseWheel(msg.X, msg.Y, -3)
			}
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
		m.lastUserAction = m.now()
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
	if snapshot := snapshotFromGitStatuses(msg.statuses); len(snapshot) > 0 {
		m.applyStatusSnapshot(snapshot, msg.statusEpoch)
	}
	if len(msg.untracked) > 0 {
		untrackedSet := make(map[string]struct{}, len(msg.untracked))
		for _, p := range msg.untracked {
			untrackedSet[filepath.ToSlash(p)] = struct{}{}
		}
		for i := range msg.files {
			name := msg.files[i].NewName
			if name == "/dev/null" {
				name = msg.files[i].OldName
			}
			if _, ok := untrackedSet[filepath.ToSlash(name)]; ok {
				msg.files[i].Untracked = true
			}
		}
	}
	// Append synthetic entries for collapsed untracked directories.
	for _, cd := range msg.collapsedDirs {
		msg.files = append(msg.files, diff.FileDiff{
			OldName:        "/dev/null",
			NewName:        cd.Dir,
			Untracked:      true,
			CollapsedCount: cd.Count,
		})
	}
	if len(m.statusSnapshot) > 0 {
		applySnapshotToFiles(msg.files, m.statusSnapshot)
	}
	msg.files = filterDiffFiles(msg.files, m.config.PathFilter)
	prevMode := m.fileList.mode
	prevReadSet := m.fileList.readSet
	prevRoot := m.fileList.root
	m.fileList = newFileList(msg.files)
	m.fileList.review = m.review
	if prevReadSet != nil {
		m.fileList.readSet = prevReadSet
	}
	if prevRoot != nil {
		m.fileList.root = prevRoot
	}
	if prevMode == fileListModeFullTree {
		m.fileList.mode = fileListModeFullTree
		m.fileList.rebuildTreeRows()
	}
	if msg.preserveSelection != "" {
		_ = m.fileList.focusPath(msg.preserveSelection)
	}
	m.commits = msg.commits
	m.updateLayout()
	m.setDiffViewForSelection(false)
}

func (m *Model) applyCommitLoaded(msg commitLoadedMsg) {
	if msg.err != nil {
		m.err = msg.err
		return
	}
	msg.files = filterDiffFiles(msg.files, m.config.PathFilter)
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
	m.expandedFiles = filterDiffFiles(msg.files, m.config.PathFilter)
	m.setDiffViewForSelection(true)
}

func (m *Model) nextStatusEpoch() int {
	m.statusEpoch++
	return m.statusEpoch
}

func (m Model) pollStatus(epoch int) tea.Cmd {
	return func() tea.Msg {
		statuses, err := m.git.Status()
		if err != nil {
			return statusPolledMsg{epoch: epoch, err: err}
		}
		return statusPolledMsg{
			snapshot: snapshotFromGitStatuses(statuses),
			epoch:    epoch,
		}
	}
}

func (m *Model) applyStatusSnapshot(snapshot map[string]statusSnapshotEntry, epoch int) bool {
	if epoch < m.statusAppliedEpoch {
		return false
	}
	m.statusSnapshot = snapshot
	m.statusAppliedEpoch = epoch
	return true
}

func shouldReloadForStatusTransition(prev, next map[string]statusSnapshotEntry, cfg Config) bool {
	if !(cfg.UnstagedOnly || cfg.RevSpec == "" || strings.HasSuffix(cfg.RevSpec, "...HEAD")) {
		return false
	}
	seen := make(map[string]struct{}, len(prev)+len(next))
	for path, oldEntry := range prev {
		seen[path] = struct{}{}
		if shouldIncludeInCurrentReview(oldEntry, cfg) != shouldIncludeInCurrentReview(next[path], cfg) {
			return true
		}
	}
	for path, cur := range next {
		if _, ok := seen[path]; ok {
			continue
		}
		if shouldIncludeInCurrentReview(cur, cfg) {
			return true
		}
	}
	return false
}

func applySnapshotToFiles(files []diff.FileDiff, snapshot map[string]statusSnapshotEntry) {
	for i := range files {
		name := files[i].NewName
		if name == "/dev/null" {
			name = files[i].OldName
		}
		entry, ok := snapshot[filepath.ToSlash(name)]
		files[i].Staged = ok && entry.Staged
		files[i].Untracked = ok && entry.Untracked
	}
}

func (m *Model) applySnapshotToVisibleFiles() {
	applySnapshotToFiles(m.fileList.files, m.statusSnapshot)
	if m.expandedFiles != nil {
		applySnapshotToFiles(m.expandedFiles, m.statusSnapshot)
	}
}

func (m *Model) applyOptimisticStageToggle(path string, staged bool) {
	if m.statusSnapshot == nil {
		m.statusSnapshot = make(map[string]statusSnapshotEntry)
	}
	path = filepath.ToSlash(path)
	entry := m.statusSnapshot[path]
	entry.Staged = staged
	if staged {
		if entry.Index == 0 || entry.Index == ' ' {
			entry.Index = 'M'
		}
	} else {
		entry.Index = ' '
	}
	m.statusSnapshot[path] = entry
	m.applySnapshotToVisibleFiles()
}

func filterDiffFiles(files []diff.FileDiff, pathFilter string) []diff.FileDiff {
	filter := normalizeFilter(pathFilter)
	if filter == "" {
		return files
	}
	out := make([]diff.FileDiff, 0, len(files))
	for i := range files {
		p := files[i].NewName
		if p == "/dev/null" {
			p = files[i].OldName
		}
		p = normalizeFilter(p)
		if matchesPathFilter(p, filter) {
			out = append(out, files[i])
		}
	}
	return out
}

func matchesPathFilter(filePath, filter string) bool {
	if filePath == "" || filter == "" {
		return false
	}
	if strings.ContainsAny(filter, "*?[") {
		ok, err := path.Match(filter, filePath)
		return err == nil && ok
	}
	return filePath == filter || strings.HasPrefix(filePath, filter+"/")
}

func normalizeFilter(v string) string {
	v = filepath.ToSlash(strings.TrimSpace(v))
	for strings.HasPrefix(v, "./") {
		v = strings.TrimPrefix(v, "./")
	}
	clean := path.Clean(v)
	if clean == "." {
		return ""
	}
	return clean
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

func (m *Model) canToggleStage() bool {
	if m.config.UnstagedOnly {
		return true
	}
	if m.config.RevSpec == "" {
		return true
	}
	return strings.HasSuffix(m.config.RevSpec, "...HEAD")
}

func (m Model) toggleStageForPath(path string, staged bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		action := "Staged " + path
		nextStaged := true
		if staged {
			action = "Unstaged " + path
			nextStaged = false
			err = m.git.Unstage(path)
		} else {
			err = m.git.Stage(path)
		}
		return stageToggledMsg{
			action:            action,
			path:              path,
			staged:            nextStaged,
			preserveSelection: path,
			err:               err,
		}
	}
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = "" // clear transient status on any keypress
	k := msg.String()

	if m.focus == focusGeneralPanel {
		if handledModel, cmd, handled := m.handleGeneralPanelFocusKey(msg); handled {
			return handledModel, cmd
		}
	}

	// Handle 'gg' sequence: second 'g' goes to top
	if m.pendingG {
		m.pendingG = false
		if k == "g" {
			hasCount := m.countBuf != ""
			count := m.consumeCount()
			if hasCount {
				// [count]gg = go to source line number
				m.diffView.moveCursorToLineNum(count)
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

	hasCount := m.countBuf != ""
	count := m.consumeCount()
	if k == "G" {
		if hasCount {
			// [count]G = go to source line number
			m.diffView.moveCursorToLineNum(count)
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
	case key.Matches(msg, keys.PageDown):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.scrollByRows(count * pageRows)
	case key.Matches(msg, keys.PageUp):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.scrollByRows(-count * pageRows)
	case isWindowPageDownKey(msg):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.windowScrollByRows(count * pageRows)
	case isWindowPageUpKey(msg):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.windowScrollByRows(-count * pageRows)

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
	case key.Matches(msg, keys.NextUnreadFile):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		for i := 0; i < count; i++ {
			if !m.fileList.nextUnread() {
				break
			}
		}
		m.setDiffViewForSelection(false)
	case key.Matches(msg, keys.PrevUnreadFile):
		m.fileStates[m.currentStateKey()] = m.diffView.saveState()
		for i := 0; i < count; i++ {
			if !m.fileList.prevUnread() {
				break
			}
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
		if m.focus == focusGeneralPanel {
			m.focus = focusDiff
		} else {
			m.focus = focusGeneralPanel
			m.generalPanel.focused = true
			m.generalPanel.clampScroll()
		}
		m.updateLayout()
		return m, nil

	case key.Matches(msg, keys.GeneralComment):
		return m.openGeneralCommentEditor()

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

	case key.Matches(msg, keys.ToggleRead):
		read, ok := m.fileList.toggleReadSelected()
		if !ok {
			break
		}
		if read {
			m.statusMsg = "Marked read"
		} else {
			m.statusMsg = "Marked unread"
		}

	case key.Matches(msg, keys.ToggleStage):
		if !m.canToggleStage() {
			m.statusMsg = "Stage toggle unavailable for commit diffs"
			break
		}
		path, idx, ok := m.fileList.modifiedSelection()
		if !ok || idx < 0 || idx >= len(m.fileList.files) {
			break
		}
		m.gitOpInFlight = true
		return m, m.toggleStageForPath(path, m.fileList.files[idx].Staged)

	case key.Matches(msg, keys.Save):
		return m.beginSaveAndQuit()
	}

	return m, nil
}

func (m Model) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
	headerH := lipgloss.Height(m.renderHeader())
	yRel := y - headerH
	if yRel < 0 {
		m.mouseDrag.active = false
		return m, nil
	}

	bodyHeight := m.diffView.height + 2
	if yRel < bodyHeight {
		m.focus = focusDiff
		// Body starts at y == headerH. Panels have a 1-line border at top.
		bodyY := yRel - 1 // row index within panel content area
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
			// Refresh visible row/comment maps on the live model before hit-testing.
			if m.diffView.mode == viewUnified {
				_ = m.diffView.renderUnifiedContent()
			} else {
				_ = m.diffView.renderSideBySideContent()
			}

			row := m.diffView.scrollY + diffY
			if row >= 0 && row < len(m.diffView.rows) {
				m.mouseDrag.active = true
				m.mouseDrag.startRow = row
			}
			clickedComment := m.diffView.commentAtScreenY(diffY)
			m.diffView.clickAt(diffY)
			now := m.now()
			if c := clickedComment; c != nil &&
				!m.lastDiffClick.at.IsZero() &&
				now.Sub(m.lastDiffClick.at) <= mouseDoubleClickThreshold &&
				m.lastDiffClick.key == inlineCommentKey(c) {
				m.lastDiffClick.at = time.Time{}
				m.lastDiffClick.key = ""
				return m.openInlineCommentEditor(c)
			}
			m.lastDiffClick.at = now
			if clickedComment != nil {
				m.lastDiffClick.key = inlineCommentKey(clickedComment)
			} else {
				m.lastDiffClick.key = ""
			}
		}
		return m, nil
	}

	yRel -= bodyHeight
	if m.generalPanelVisible() {
		panelHeight := m.generalPanel.height + 2
		if yRel < panelHeight {
			m.mouseDrag.active = false
			m.focus = focusGeneralPanel
			m.generalPanel.focused = true
			panelY := yRel - 1
			if panelY >= 0 && panelY < m.generalPanel.height {
				m.generalPanel.clickAt(panelY)
				now := m.now()
				if !m.lastGeneralClick.at.IsZero() &&
					now.Sub(m.lastGeneralClick.at) <= mouseDoubleClickThreshold &&
					m.lastGeneralClick.panelY == panelY {
					m.lastGeneralClick.at = time.Time{}
					return m.openGeneralCommentEditor()
				}
				m.lastGeneralClick.at = now
				m.lastGeneralClick.panelY = panelY
			}
			m.updateLayout()
			return m, nil
		}
	}

	m.mouseDrag.active = false
	return m, nil
}

func (m Model) openGeneralCommentEditor() (tea.Model, tea.Cmd) {
	m.mode = modeGeneralComment
	if m.review.GeneralComment() != "" {
		m.generalEditIdx = 0
		m.generalInput = m.newGeneralTextarea(m.review.GeneralComment())
	} else {
		m.generalEditIdx = -1
		m.generalInput = m.newGeneralTextarea("")
	}
	return m, m.generalInput.Focus()
}

func inlineCommentKey(c *review.Comment) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d", c.File, c.Line, c.EndLine)
}

func (m Model) openInlineCommentEditor(c *review.Comment) (tea.Model, tea.Cmd) {
	if c == nil {
		return m, nil
	}
	m.mode = modeComment
	if c.EndLine > 0 {
		m.diffView.activateEditRangeComment(c.File, c.Line, c.EndLine, c.Text)
	} else {
		m.diffView.activateEditComment(c.File, c.Line, c.Text)
	}
	return m, m.diffView.commentInput.Focus()
}

func (m Model) handleGeneralPanelFocusKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	k := msg.String()
	total := len(m.review.GeneralComments)

	switch {
	case key.Matches(msg, keys.ViewGeneral), key.Matches(msg, keys.Cancel):
		m.focus = focusDiff
		m.generalPanel.focused = false
		m.updateLayout()
		return m, nil, true
	case k == "j" || k == "down":
		m.generalPanel.moveSelection(1)
		return m, nil, true
	case k == "k" || k == "up":
		m.generalPanel.moveSelection(-1)
		return m, nil, true
	case key.Matches(msg, keys.DeleteComment):
		if total > 0 {
			m.review.DeleteGeneralComment(0)
			m.generalPanel.clampScroll()
			m.dirty = true
			m.statusMsg = "General comment deleted"
			m.updateLayout()
		}
		return m, nil, true
	case key.Matches(msg, keys.EditComment):
		if total > 0 {
			updated, cmd := m.openGeneralCommentEditor()
			return updated.(Model), cmd, true
		}
		return m, nil, true
	case key.Matches(msg, keys.GeneralComment):
		updated, cmd := m.openGeneralCommentEditor()
		return updated.(Model), cmd, true
	}
	return m, nil, false
}

func (m Model) handleMouseWheel(x, y, delta int) (tea.Model, tea.Cmd) {
	if delta == 0 {
		return m, nil
	}

	headerH := lipgloss.Height(m.renderHeader())
	yRel := y - headerH
	if yRel < 0 {
		return m, nil
	}

	bodyHeight := m.diffView.height + 2
	if yRel < bodyHeight {
		// Only scroll when pointer is over diff panel, not file list.
		if x > m.fileListWidth+1 {
			m.focus = focusDiff
			m.generalPanel.focused = false
			m.diffView.scrollByRows(delta)
		}
		return m, nil
	}

	yRel -= bodyHeight
	if m.generalPanelVisible() {
		panelHeight := m.generalPanel.height + 2
		if yRel < panelHeight {
			m.focus = focusGeneralPanel
			m.generalPanel.focused = true
			m.generalPanel.moveSelection(delta)
			m.updateLayout()
		}
	}
	return m, nil
}

func (m *Model) syncGeneralPanelAfterSubmit() {
	m.generalPanel.scrollY = 0
	m.generalPanel.clampScroll()
}

func (m Model) generalPanelVisible() bool {
	return true
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
		m.mouseDrag.active = false
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
			hasCount := m.countBuf != ""
			count := m.consumeCount()
			if hasCount {
				m.diffView.moveCursorToLineNum(count)
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

	hasCount := m.countBuf != ""
	count := m.consumeCount()
	if k == "G" {
		if hasCount {
			m.diffView.moveCursorToLineNum(count)
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
	case key.Matches(msg, keys.PageDown):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.scrollByRows(count * pageRows)
	case key.Matches(msg, keys.PageUp):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.scrollByRows(-count * pageRows)
	case isWindowPageDownKey(msg):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.windowScrollByRows(count * pageRows)
	case isWindowPageUpKey(msg):
		pageRows := m.diffView.logicalRowsForScreenLines(m.diffView.scrollY, m.diffView.contentViewportHeight())
		if pageRows < 1 {
			pageRows = 1
		}
		m.diffView.windowScrollByRows(-count * pageRows)

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
			m.review.AddGeneralComment(text)
			m.statusMsg = "General comment saved"
			m.dirty = true
			m.syncGeneralPanelAfterSubmit()
			m.updateLayout()
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
	ta.Placeholder = "Enter comment... (enter=newline, ctrl+s=submit, ctrl+g=editor)"
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

func (m Model) viewGeneralCommentInput() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorYellow).
		Render("General Comment")

	hint := lipgloss.NewStyle().Foreground(colorDim).Render(
		"ctrl+s submit | ctrl+g editor | esc cancel")

	content := title + "\n" + m.generalInput.View() + "\n" + hint

	return generalPanelStyle.
		BorderForeground(colorYellow).
		Width(m.width).
		Render(content)
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

func isWindowPageDownKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, keys.WindowPageDown) || msg.Type == tea.KeyCtrlF
}

func isWindowPageUpKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, keys.WindowPageUp) || msg.Type == tea.KeyCtrlB
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
		{"PgDn / PgUp", "Page down/up (cursor-anchored)"},
		{"Ctrl+f / Ctrl+b", "Window scroll page down/up"},
		{"/", "Search in diff"},
		{"f", "Find file by name/path"},
		{"p", "Find file by content"},
		{"n / N", "Next / previous match"},
		{"] / [", "Next / previous file (includes read)"},
		{"→ / ←", "Next / previous unread file"},
		{"m", "Mark selected file read/unread"},
		{"a", "Stage / unstage selected modified file"},
		{"^", "File list marker: file has staged changes"},
		{"} / {", "Next / previous modified file"},
		{"M", "Jump to first modified file"},
		{"t", "Toggle changed / all files"},
		{"o", "Open/close selected directory"},
		{"< / >", "Shrink / grow file panel"},
		{"h / l", "Previous / next commit"},
		{"V", "Visual select (then c to comment)"},
		{"c", "Add inline comment at cursor"},
		{"R", "Edit general comment (multi-line)"},
		{"Ctrl+r", "Focus general comments panel"},
		{"j/k, d, E", "When panel focused: nav/delete/edit"},
		{"Ctrl+s", "Submit comment"},
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
	m.config.UnstagedOnly = false
	applyDiffContext(m.review, commit.Hash, false)
	m.review.CommitHash = commit.Hash
	m.review.CommitSubject = commit.Subject

	return func() tea.Msg {
		rawDiff, _, err := m.git.Diff(commit.Hash)
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
	generalH := 0
	m.generalPanel.width = m.width
	m.generalPanel.review = m.review
	m.generalPanel.focused = m.focus == focusGeneralPanel
	if m.mode == modeGeneralComment {
		generalH = generalCommentInputContentHeight + 2
	} else if m.generalPanelVisible() {
		m.generalPanel.height = generalPanelContentHeight
		m.generalPanel.clampScroll()
		generalH = m.generalPanel.height + 2
	} else {
		m.generalPanel.height = 0
	}
	contentHeight := m.height - headerH - footerH - panelBorderH - extraLines - generalH
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
	} else if m.config.UnstagedOnly {
		headerText = " [unstaged diff] unstaged + untracked"
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
	footerHints := fmt.Sprintf(" `j/k`move `H/L`screen-top/bot `gg/G`top/bot `PgDn/Up`page `^f/^b`scroll `/`search `n/N`next/prev `V`visual `c`comment `R`general `^r`focus panel `tab`%s `e`expand `s`save `q`quit `?`help",
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
	navParts := []string{
		"`f`find",
		"`p`content",
		"`]/[`next/prev",
		"`←/→`next/prev(unread)",
		"`m`toggle read",
		"`a`stage/unstage",
		"`h/l`commit",
		"`e`expand",
		"`t`changed/all",
	}
	if m.fileList.mode == fileListModeFullTree {
		navParts = append(navParts, "`}/{`modified", "`M`first-modified", "`o`dir")
	}
	navParts = append(navParts, "`</>`resize")
	navInfo := fmt.Sprintf(" %s  [%s] %s  %s",
		strings.Join(navParts, " "), fileMode, fileCount, commentCount)
	navInfo += "  `^`staged"
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

	parts := []string{header, body}
	if m.mode == modeGeneralComment {
		parts = append(parts, m.viewGeneralCommentInput())
	} else if m.generalPanelVisible() {
		parts = append(parts, m.generalPanel.view())
	}
	if hasBottomBar {
		parts = append(parts, m.bottomBar.view())
	}
	parts = append(parts, footer)
	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

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
