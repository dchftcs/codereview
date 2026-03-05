package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/git"
	"github.com/dc/codereview/internal/review"
)

type mode int

const (
	modeNormal mode = iota
	modeComment
)

// Config holds TUI startup configuration.
type Config struct {
	RevSpec    string
	OutputFile string
	Highlight  func(filename, content string) string
}

// SaveMsg is sent when the user wants to save the review.
type SaveMsg struct {
	Review *review.Review
	Output string
}

type Model struct {
	config    Config
	mode      mode
	width     int
	height    int
	fileList  fileList
	diffView  diffView
	comment   commentInput
	review    *review.Review
	commits   []git.CommitInfo
	commitIdx int
	expanded  bool
	err       error
	quitting  bool
	saving    bool
}

func NewModel(cfg Config) Model {
	m := Model{
		config:   cfg,
		review:   review.New(),
		diffView: newDiffView(),
		comment:  newCommentInput(),
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return m.loadDiff()
}

type diffLoadedMsg struct {
	files   []diff.FileDiff
	commits []git.CommitInfo
	err     error
}

func (m Model) loadDiff() tea.Cmd {
	return func() tea.Msg {
		rawDiff, err := git.Diff(m.config.RevSpec)
		if err != nil {
			return diffLoadedMsg{err: err}
		}

		files, err := diff.Parse(rawDiff)
		if err != nil {
			return diffLoadedMsg{err: err}
		}

		commits, _ := git.Log(50)

		return diffLoadedMsg{files: files, commits: commits}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case diffLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.fileList = newFileList(msg.files)
		m.commits = msg.commits
		m.updateLayout()
		if f := m.fileList.selectedFile(); f != nil {
			m.diffView.setFile(f, m.review)
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeComment {
			return m.updateComment(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, keys.Down):
		m.diffView.scrollDown(1)
	case key.Matches(msg, keys.Up):
		m.diffView.scrollUp(1)
	case key.Matches(msg, keys.HalfPageDown):
		m.diffView.scrollDown(m.diffView.height / 2)
	case key.Matches(msg, keys.HalfPageUp):
		m.diffView.scrollUp(m.diffView.height / 2)

	case key.Matches(msg, keys.NextFile):
		m.fileList.next()
		if f := m.fileList.selectedFile(); f != nil {
			m.diffView.setFile(f, m.review)
		}
	case key.Matches(msg, keys.PrevFile):
		m.fileList.prev()
		if f := m.fileList.selectedFile(); f != nil {
			m.diffView.setFile(f, m.review)
		}

	case key.Matches(msg, keys.NextCommit):
		return m.navigateCommit(1), nil
	case key.Matches(msg, keys.PrevCommit):
		return m.navigateCommit(-1), nil

	case key.Matches(msg, keys.Comment):
		m.mode = modeComment
		file := m.currentFileName()
		lineNum := m.diffView.currentLineNum()
		m.comment.activate(file, lineNum)
		return m, m.comment.input.Focus()

	case key.Matches(msg, keys.DeleteComment):
		file := m.currentFileName()
		lineNum := m.diffView.currentLineNum()
		m.review.DeleteComment(file, lineNum)
		m.diffView.buildRows()

	case key.Matches(msg, keys.Expand):
		m.expanded = !m.expanded
		m.updateLayout()

	case key.Matches(msg, keys.ToggleView):
		m.diffView.toggleMode()

	case key.Matches(msg, keys.Save):
		m.saving = true
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) updateComment(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.comment.deactivate()
		return m, nil

	case key.Matches(msg, keys.Submit):
		text := m.comment.value()
		if text != "" {
			m.review.AddComment(m.comment.file, m.comment.line, text)
			m.diffView.buildRows()
		}
		m.mode = modeNormal
		m.comment.deactivate()
		return m, nil
	}

	// Forward to text input
	var cmd tea.Cmd
	m.comment.input, cmd = m.comment.input.Update(msg)
	return m, cmd
}

func (m *Model) navigateCommit(delta int) Model {
	newIdx := m.commitIdx + delta
	if newIdx < 0 || newIdx >= len(m.commits) {
		return *m
	}
	m.commitIdx = newIdx
	commit := m.commits[m.commitIdx]
	m.config.RevSpec = commit.Hash
	m.review.CommitHash = commit.Hash
	m.review.CommitSubject = commit.Subject

	rawDiff, err := git.Diff(commit.Hash)
	if err != nil {
		m.err = err
		return *m
	}
	files, err := diff.Parse(rawDiff)
	if err != nil {
		m.err = err
		return *m
	}
	m.fileList = newFileList(files)
	m.updateLayout()
	if f := m.fileList.selectedFile(); f != nil {
		m.diffView.setFile(f, m.review)
	}
	return *m
}

func (m *Model) updateLayout() {
	fileListWidth := 20
	if m.expanded {
		fileListWidth = 0
	}
	m.fileList.height = m.height - 4 // header + footer
	m.diffView.width = m.width - fileListWidth - 2
	m.diffView.height = m.height - 4
}

func (m Model) currentFileName() string {
	f := m.fileList.selectedFile()
	if f == nil {
		return ""
	}
	name := f.NewName
	if name == "/dev/null" {
		name = f.OldName
	}
	return name
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}
	if m.quitting {
		return ""
	}

	// Header
	headerText := " cr"
	if len(m.commits) > 0 && m.commitIdx < len(m.commits) {
		c := m.commits[m.commitIdx]
		headerText = fmt.Sprintf(" [%s] %s", c.Hash, c.Subject)
	} else if m.config.RevSpec == "" {
		headerText = " [working tree] uncommitted changes"
	}
	header := headerStyle.Width(m.width).Render(headerText)

	// Footer
	modeStr := "side-by-side"
	if m.diffView.mode == viewUnified {
		modeStr = "unified"
	}
	fileCount := fmt.Sprintf("%d/%d files", m.fileList.selected+1, len(m.fileList.files))
	commentCount := fmt.Sprintf("%d comments", len(m.review.Comments))
	footer := footerStyle.Width(m.width).Render(
		fmt.Sprintf(" [j/k]scroll [n/N]file [h/l]commit [c]omment [d]elete [tab]%s [e]xpand [s]ave [q]uit  %s  %s",
			modeStr, fileCount, commentCount))

	// Body
	var body string
	if m.expanded {
		body = m.diffView.view()
	} else {
		fileListWidth := 20
		fl := m.fileList.view(fileListWidth)
		dv := m.diffView.view()
		body = lipgloss.JoinHorizontal(lipgloss.Top, fl, dv)
	}

	// Comment input
	if m.mode == modeComment {
		return lipgloss.JoinVertical(lipgloss.Left, header, body, m.comment.view(), footer)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// GetReview returns the review data (used after quit).
func (m Model) GetReview() *review.Review {
	return m.review
}

// IsSaving returns whether the user triggered a save.
func (m Model) IsSaving() bool {
	return m.saving
}
