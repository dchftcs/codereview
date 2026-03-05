package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/review"
)

type viewMode int

const (
	viewSideBySide viewMode = iota
	viewUnified
)

type diffView struct {
	file      *diff.FileDiff
	mode      viewMode
	scrollY   int
	height    int
	width     int
	highlight func(filename, content string) string // injected highlighter
	comments  *review.Review
	// Flattened rows for scrolling
	rows []diffRow
}

type diffRowKind int

const (
	rowDiffPair diffRowKind = iota
	rowHunkHeader
	rowComment
)

type diffRow struct {
	kind    diffRowKind
	pair    *diff.LinePair   // for rowDiffPair
	hunk    string           // for rowHunkHeader
	comment *review.Comment  // for rowComment
	lineNum int              // the line this row is associated with (for cursor)
}

func newDiffView() diffView {
	return diffView{mode: viewSideBySide}
}

func (dv *diffView) setFile(f *diff.FileDiff, rev *review.Review) {
	dv.file = f
	dv.comments = rev
	dv.scrollY = 0
	dv.buildRows()
}

func (dv *diffView) buildRows() {
	dv.rows = nil
	if dv.file == nil {
		return
	}

	filename := dv.file.NewName
	if filename == "/dev/null" {
		filename = dv.file.OldName
	}

	for _, hunk := range dv.file.Hunks {
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		if hunk.Section != "" {
			header += " " + hunk.Section
		}
		dv.rows = append(dv.rows, diffRow{kind: rowHunkHeader, hunk: header})

		for i := range hunk.Pairs {
			pair := &hunk.Pairs[i]
			lineNum := 0
			if pair.Right != nil {
				lineNum = pair.Right.NewNum
			} else if pair.Left != nil {
				lineNum = pair.Left.OldNum
			}
			dv.rows = append(dv.rows, diffRow{kind: rowDiffPair, pair: pair, lineNum: lineNum})

			// Insert comments after this line
			if dv.comments != nil {
				for _, c := range dv.comments.Comments {
					if c.File == filename && c.Line == lineNum {
						dv.rows = append(dv.rows, diffRow{kind: rowComment, comment: &c, lineNum: lineNum})
					}
				}
			}
		}
	}
}

func (dv *diffView) scrollDown(n int) {
	dv.scrollY += n
	maxScroll := len(dv.rows) - dv.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if dv.scrollY > maxScroll {
		dv.scrollY = maxScroll
	}
}

func (dv *diffView) scrollUp(n int) {
	dv.scrollY -= n
	if dv.scrollY < 0 {
		dv.scrollY = 0
	}
}

func (dv *diffView) currentLineNum() int {
	if len(dv.rows) == 0 {
		return 0
	}
	idx := dv.scrollY
	if idx >= len(dv.rows) {
		idx = len(dv.rows) - 1
	}
	return dv.rows[idx].lineNum
}

func (dv *diffView) toggleMode() {
	if dv.mode == viewSideBySide {
		dv.mode = viewUnified
	} else {
		dv.mode = viewSideBySide
	}
}

func (dv *diffView) view() string {
	if dv.file == nil {
		return diffPanelStyle.Width(dv.width).Height(dv.height).Render("No file selected")
	}

	if dv.mode == viewUnified {
		return dv.renderUnified()
	}
	return dv.renderSideBySide()
}

func (dv *diffView) renderSideBySide() string {
	colWidth := (dv.width - 4) / 2 // account for border + separator
	if colWidth < 20 {
		colWidth = 20
	}

	var lines []string
	end := dv.scrollY + dv.height
	if end > len(dv.rows) {
		end = len(dv.rows)
	}

	for i := dv.scrollY; i < end; i++ {
		row := dv.rows[i]
		switch row.kind {
		case rowHunkHeader:
			lines = append(lines, hunkHeaderStyle.Width(dv.width-4).Render(row.hunk))
		case rowComment:
			commentText := fmt.Sprintf("💬 %s", row.comment.Text)
			lines = append(lines, commentBorderStyle.Width(dv.width-6).Render(commentText))
		case rowDiffPair:
			pair := row.pair
			left := dv.renderSide(pair.Left, colWidth, true)
			right := dv.renderSide(pair.Right, colWidth, false)
			sep := lipgloss.NewStyle().Foreground(colorDim).Render("│")
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right))
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return diffPanelStyle.Width(dv.width).Height(dv.height).Render(content)
}

func (dv *diffView) renderSide(line *diff.DiffLine, width int, isLeft bool) string {
	if line == nil {
		return lipgloss.NewStyle().Width(width).Render("")
	}

	num := line.OldNum
	if !isLeft {
		num = line.NewNum
	}

	numStr := lineNumStyle.Render(fmt.Sprintf("%d", num))
	content := line.Content

	// Apply highlighting if available
	if dv.highlight != nil && dv.file != nil {
		filename := dv.file.NewName
		if filename == "/dev/null" {
			filename = dv.file.OldName
		}
		content = dv.highlight(filename, content)
	}

	var style lipgloss.Style
	switch line.Op {
	case diff.OpDelete:
		style = removedStyle
	case diff.OpInsert:
		style = addedStyle
	default:
		style = lipgloss.NewStyle()
	}

	textWidth := width - 6 // line number width + space
	text := style.Render(truncate(content, textWidth))
	return lipgloss.JoinHorizontal(lipgloss.Top, numStr, " ", text)
}

func (dv *diffView) renderUnified() string {
	var lines []string
	end := dv.scrollY + dv.height
	if end > len(dv.rows) {
		end = len(dv.rows)
	}

	contentWidth := dv.width - 16 // line numbers + margins

	for i := dv.scrollY; i < end; i++ {
		row := dv.rows[i]
		switch row.kind {
		case rowHunkHeader:
			lines = append(lines, hunkHeaderStyle.Width(dv.width-4).Render(row.hunk))
		case rowComment:
			commentText := fmt.Sprintf("💬 %s", row.comment.Text)
			lines = append(lines, commentBorderStyle.Width(dv.width-6).Render(commentText))
		case rowDiffPair:
			pair := row.pair
			// In unified mode, show delete then insert
			if pair.Left != nil && pair.Left.Op == diff.OpDelete {
				lines = append(lines, dv.renderUnifiedLine(pair.Left, contentWidth))
			}
			if pair.Right != nil && pair.Right.Op == diff.OpInsert {
				lines = append(lines, dv.renderUnifiedLine(pair.Right, contentWidth))
			}
			// For equal lines, just show once
			if pair.Left != nil && pair.Left.Op == diff.OpEqual {
				lines = append(lines, dv.renderUnifiedLine(pair.Left, contentWidth))
			}
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return diffPanelStyle.Width(dv.width).Height(dv.height).Render(content)
}

func (dv *diffView) renderUnifiedLine(line *diff.DiffLine, width int) string {
	oldNum := "     "
	newNum := "     "
	prefix := " "
	style := lipgloss.NewStyle()

	switch line.Op {
	case diff.OpEqual:
		oldNum = fmt.Sprintf("%5d", line.OldNum)
		newNum = fmt.Sprintf("%5d", line.NewNum)
	case diff.OpDelete:
		oldNum = fmt.Sprintf("%5d", line.OldNum)
		prefix = "-"
		style = removedStyle
	case diff.OpInsert:
		newNum = fmt.Sprintf("%5d", line.NewNum)
		prefix = "+"
		style = addedStyle
	}

	numStr := lipgloss.NewStyle().Foreground(colorDim).Render(oldNum + " " + newNum)
	content := line.Content
	if dv.highlight != nil && dv.file != nil {
		filename := dv.file.NewName
		if filename == "/dev/null" {
			filename = dv.file.OldName
		}
		content = dv.highlight(filename, content)
	}
	text := style.Render(prefix + truncate(content, width))
	return numStr + " " + text
}

func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	// Simple rune-based truncation
	runes := []rune(s)
	if len(runes) > maxWidth {
		return string(runes[:maxWidth-1]) + "…"
	}
	return s
}

// padRight pads a string with spaces to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
