package tui

import "github.com/charmbracelet/lipgloss"

// ThemeName identifies a color theme.
type ThemeName string

const (
	ThemeDark  ThemeName = "dark"
	ThemeLight ThemeName = "light"
)

type palette struct {
	red, green, yellow, blue, purple, dim, bg, bgLight, fg lipgloss.Color
	addedBg, removedBg, commentRangeBg                     lipgloss.Color
}

var darkPalette = palette{
	red:            lipgloss.Color("#ff5555"),
	green:          lipgloss.Color("#50fa7b"),
	yellow:         lipgloss.Color("#f1fa8c"),
	blue:           lipgloss.Color("#8be9fd"),
	purple:         lipgloss.Color("#bd93f9"),
	dim:            lipgloss.Color("#6272a4"),
	bg:             lipgloss.Color("#282a36"),
	bgLight:        lipgloss.Color("#44475a"),
	fg:             lipgloss.Color("#f8f8f2"),
	addedBg:        lipgloss.Color("#1a2e1a"),
	removedBg:      lipgloss.Color("#2e1a1a"),
	commentRangeBg: lipgloss.Color("#2a2040"),
}

var lightPalette = palette{
	red:            lipgloss.Color("#cc0000"),
	green:          lipgloss.Color("#007700"),
	yellow:         lipgloss.Color("#886600"),
	blue:           lipgloss.Color("#0055aa"),
	purple:         lipgloss.Color("#6a0dad"),
	dim:            lipgloss.Color("#888888"),
	bg:             lipgloss.Color("#f5f5dc"),
	bgLight:        lipgloss.Color("#d8d8c0"),
	fg:             lipgloss.Color("#333333"),
	addedBg:        lipgloss.Color("#d4f5d4"),
	removedBg:      lipgloss.Color("#f5d4d4"),
	commentRangeBg: lipgloss.Color("#ece0f5"),
}

var (
	// Colors — set by applyTheme
	colorRed     lipgloss.Color
	colorGreen   lipgloss.Color
	colorYellow  lipgloss.Color
	colorBlue    lipgloss.Color
	colorPurple  lipgloss.Color
	colorDim     lipgloss.Color
	colorBg      lipgloss.Color
	colorBgLight lipgloss.Color
	colorFg      lipgloss.Color

	// Styles — set by applyTheme
	fileListStyle            lipgloss.Style
	diffPanelStyle           lipgloss.Style
	generalPanelStyle        lipgloss.Style
	headerStyle              lipgloss.Style
	footerStyle              lipgloss.Style
	addedStyle               lipgloss.Style
	removedStyle             lipgloss.Style
	lineNumStyle             lipgloss.Style
	selectedFileStyle        lipgloss.Style
	normalFileStyle          lipgloss.Style
	commentBorderStyle       lipgloss.Style
	commentPromptStyle       lipgloss.Style
	hunkHeaderStyle          lipgloss.Style
	cursorStyle              lipgloss.Style
	addedBgStyle             lipgloss.Style
	removedBgStyle           lipgloss.Style
	commentRangeBgStyle      lipgloss.Style
	contextMenuBoxStyle      lipgloss.Style
	contextMenuItemStyle     lipgloss.Style
	contextMenuSelectedStyle lipgloss.Style
)

func init() {
	applyTheme(ThemeDark)
}

func applyTheme(name ThemeName) {
	p := darkPalette
	if name == ThemeLight {
		p = lightPalette
	}

	colorRed = p.red
	colorGreen = p.green
	colorYellow = p.yellow
	colorBlue = p.blue
	colorPurple = p.purple
	colorDim = p.dim
	colorBg = p.bg
	colorBgLight = p.bgLight
	colorFg = p.fg

	fileListStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(0, 1)

	diffPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim)

	generalPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
		Background(colorPurple).
		Foreground(lipgloss.Color("#000")).
		Bold(true).
		Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
		Foreground(colorDim)

	addedStyle = lipgloss.NewStyle().
		Foreground(colorGreen)

	removedStyle = lipgloss.NewStyle().
		Foreground(colorRed)

	lineNumStyle = lipgloss.NewStyle().
		Foreground(colorDim).
		Width(5).
		Align(lipgloss.Right)

	selectedFileStyle = lipgloss.NewStyle().
		Background(colorBgLight).
		Foreground(colorBlue).
		Bold(true)

	normalFileStyle = lipgloss.NewStyle().
		Foreground(colorFg)

	commentBorderStyle = lipgloss.NewStyle().
		Foreground(colorYellow).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Padding(0, 1)

	commentPromptStyle = lipgloss.NewStyle().
		Foreground(colorYellow).
		Bold(true)

	hunkHeaderStyle = lipgloss.NewStyle().
		Foreground(colorPurple).
		Bold(true)

	cursorBg := lipgloss.Color("#3d3f4b")
	if name == ThemeLight {
		cursorBg = lipgloss.Color("#c8c8b0")
	}
	cursorStyle = lipgloss.NewStyle().
		Background(cursorBg)

	addedBgStyle = lipgloss.NewStyle().
		Background(p.addedBg)

	removedBgStyle = lipgloss.NewStyle().
		Background(p.removedBg)

	commentRangeBgStyle = lipgloss.NewStyle().
		Background(p.commentRangeBg)

	contextMenuBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.purple).
		Background(p.bg).
		Padding(0, 1)

	contextMenuItemStyle = lipgloss.NewStyle().
		Foreground(p.fg).
		Background(p.bg)

	contextMenuSelectedStyle = lipgloss.NewStyle().
		Foreground(p.blue).
		Background(p.bgLight).
		Bold(true)
}
