package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/review"
)

const generalPanelContentHeight = 5

type generalPanel struct {
	review  *review.Review
	height  int
	width   int
	scrollY int
	focused bool
}

func (gp *generalPanel) moveSelection(n int) {
	if gp.totalLines() == 0 {
		gp.scrollY = 0
		return
	}
	gp.scrollY += n
	gp.clampScroll()
}

func (gp *generalPanel) clampScroll() {
	total := gp.totalLines()
	if total <= gp.height {
		gp.scrollY = 0
		return
	}
	maxScroll := total - gp.height
	if gp.scrollY < 0 {
		gp.scrollY = 0
	}
	if gp.scrollY > maxScroll {
		gp.scrollY = maxScroll
	}
}

func (gp *generalPanel) clickAt(y int) bool {
	return y >= 0 && y < gp.height
}

func (gp *generalPanel) totalLines() int {
	if gp.review == nil {
		return 0
	}
	text := gp.review.GeneralComment()
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func (gp *generalPanel) view() string {
	if gp.height <= 0 {
		return ""
	}

	contentW := gp.width - 4
	if contentW < 1 {
		contentW = 1
	}

	text := gp.review.GeneralComment()
	linesSrc := []string{}
	if text != "" {
		linesSrc = strings.Split(text, "\n")
	}
	lines := make([]string, 0, gp.height)
	for i := 0; i < gp.height; i++ {
		idx := gp.scrollY + i
		line := ""
		if len(linesSrc) == 0 && i == 0 {
			line = lipgloss.NewStyle().Foreground(colorDim).Render("No general comment. Press R to add one.")
		} else if idx < len(linesSrc) {
			line = truncateToWidth(linesSrc[idx], contentW)
		}
		lines = append(lines, line)
	}

	style := generalPanelStyle
	if gp.focused {
		style = style.BorderForeground(colorPurple)
	} else {
		style = style.BorderForeground(colorDim)
	}
	return style.Width(gp.width).Height(gp.height).Render(strings.Join(lines, "\n"))
}
