package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorRed     = lipgloss.Color("#ff5555")
	colorGreen   = lipgloss.Color("#50fa7b")
	colorYellow  = lipgloss.Color("#f1fa8c")
	colorBlue    = lipgloss.Color("#8be9fd")
	colorPurple  = lipgloss.Color("#bd93f9")
	colorDim     = lipgloss.Color("#6272a4")
	colorBg      = lipgloss.Color("#282a36")
	colorBgLight = lipgloss.Color("#44475a")

	// Panels
	fileListStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	diffPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim)

	// Header / Footer
	headerStyle = lipgloss.NewStyle().
			Background(colorPurple).
			Foreground(lipgloss.Color("#000")).
			Bold(true).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	// Diff lines
	addedStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	removedStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	lineNumStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Width(5).
			Align(lipgloss.Right)

	// File list
	selectedFileStyle = lipgloss.NewStyle().
				Background(colorBgLight).
				Foreground(colorBlue).
				Bold(true)

	normalFileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f8f8f2"))

	// Comments
	commentBorderStyle = lipgloss.NewStyle().
				Foreground(colorYellow).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorYellow).
				Padding(0, 1)

	commentPromptStyle = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true)

	// Hunk header
	hunkHeaderStyle = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true)
)
