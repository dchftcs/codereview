package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/review"
)

type viewMode int

const (
	viewSideBySide viewMode = iota
	viewUnified
)

const scrollMarginLines = 5

type lineNumMode int

const (
	lineNumBoth         lineNumMode = iota // relative + absolute
	lineNumRelativeOnly                    // relative only
	lineNumAbsoluteOnly                    // absolute only
)

type diffView struct {
	file      *diff.FileDiff
	mode      viewMode
	scrollY   int
	cursorY   int // cursor position within rows (absolute index)
	height    int
	width     int
	highlight func(filename, content string) string // injected highlighter
	comments  *review.Review
	// Flattened rows for scrolling
	rows []diffRow
	// Inline comment input
	commentActive  bool
	commentEditing bool
	commentLineNum int
	commentEndLine int // >0 for range comments
	commentFile    string
	commentInput   textarea.Model
	// Visual selection
	selectionAnchor int
	selectionActive bool
	// Line number display mode
	lineNums lineNumMode
}

type diffRowKind int

const (
	rowDiffPair diffRowKind = iota
	rowHunkHeader
	rowComment
	rowSpacer
)

type diffRow struct {
	kind    diffRowKind
	pair    *diff.LinePair  // for rowDiffPair
	hunk    string          // for rowHunkHeader
	comment *review.Comment // for rowComment
	lineNum int             // the line this row is associated with (for cursor)
}

type fileState struct {
	scrollY        int
	cursorY        int
	commentActive  bool
	commentEditing bool
	commentLineNum int
	commentFile    string
	commentInput   textarea.Model
}

func (dv *diffView) saveState() fileState {
	return fileState{
		scrollY:        dv.scrollY,
		cursorY:        dv.cursorY,
		commentActive:  dv.commentActive,
		commentEditing: dv.commentEditing,
		commentLineNum: dv.commentLineNum,
		commentFile:    dv.commentFile,
		commentInput:   dv.commentInput,
	}
}

func (dv *diffView) restoreState(fs fileState) {
	dv.scrollY = fs.scrollY
	dv.cursorY = fs.cursorY
	dv.commentActive = fs.commentActive
	dv.commentEditing = fs.commentEditing
	dv.commentLineNum = fs.commentLineNum
	dv.commentFile = fs.commentFile
	dv.commentInput = fs.commentInput
	dv.buildRows()
	dv.clampScroll()
}

func newDiffView() diffView {
	return diffView{mode: viewSideBySide, lineNums: lineNumBoth}
}

// cycleLineNumMode cycles: both → relative only → absolute only → both.
func (dv *diffView) cycleLineNumMode() {
	switch dv.lineNums {
	case lineNumBoth:
		dv.lineNums = lineNumRelativeOnly
	case lineNumRelativeOnly:
		dv.lineNums = lineNumAbsoluteOnly
	case lineNumAbsoluteOnly:
		dv.lineNums = lineNumBoth
	}
}

func (dv *diffView) showRelative() bool {
	return dv.lineNums == lineNumBoth || dv.lineNums == lineNumRelativeOnly
}

func (dv *diffView) showAbsolute() bool {
	return dv.lineNums == lineNumBoth || dv.lineNums == lineNumAbsoluteOnly
}

// relativeNumStr returns a 3-char right-aligned relative row number string.
// The cursor row shows "  0", spacers show "   ", others show distance.
func (dv *diffView) relativeNumStr(rowIdx int) string {
	if rowIdx >= 0 && rowIdx < len(dv.rows) && dv.rows[rowIdx].kind == rowSpacer {
		return "   "
	}
	if rowIdx == dv.cursorY {
		return "  0"
	}
	dist := rowIdx - dv.cursorY
	if dist < 0 {
		dist = -dist
	}
	return fmt.Sprintf("%3d", dist)
}

func (dv *diffView) setFile(f *diff.FileDiff, rev *review.Review) {
	dv.file = f
	dv.comments = rev
	dv.scrollY = 0
	dv.cursorY = 0
	dv.buildRows()
}

// setFileKeepPosition switches the file data but keeps the cursor on the same
// line number at the same screen row. Used when toggling expand.
func (dv *diffView) setFileKeepPosition(f *diff.FileDiff, rev *review.Review) {
	// Remember current line number and its screen offset
	oldLineNum := 0
	anchor := diffRowAnchor{}
	if dv.cursorY < len(dv.rows) {
		oldRow := dv.rows[dv.cursorY]
		oldLineNum = oldRow.lineNum
		anchor = anchorFromRow(oldRow)
	}
	screenRow := dv.cursorY - dv.scrollY

	dv.file = f
	dv.comments = rev
	dv.buildRows()

	// Find the same row identity in the new rows.
	newCursor := dv.findBestAnchorRow(anchor, oldLineNum, dv.cursorY)

	dv.cursorY = newCursor
	dv.scrollY = newCursor - screenRow
	dv.clampKeepPosition()
}

type diffRowAnchor struct {
	lineNum    int
	leftNum    int
	rightNum   int
	hasLeft    bool
	hasRight   bool
	isDiffPair bool
	isComment  bool
	isHunk     bool
	isSpacer   bool
}

func anchorFromRow(row diffRow) diffRowAnchor {
	a := diffRowAnchor{lineNum: row.lineNum}
	switch row.kind {
	case rowDiffPair:
		a.isDiffPair = true
		if row.pair != nil {
			if row.pair.Left != nil {
				a.hasLeft = true
				a.leftNum = row.pair.Left.OldNum
			}
			if row.pair.Right != nil {
				a.hasRight = true
				a.rightNum = row.pair.Right.NewNum
			}
		}
	case rowComment:
		a.isComment = true
	case rowHunkHeader:
		a.isHunk = true
	case rowSpacer:
		a.isSpacer = true
	}
	return a
}

func (dv *diffView) findBestAnchorRow(anchor diffRowAnchor, fallbackLineNum int, preferredRow int) int {
	bestExact := -1
	bestPairShape := -1
	bestLineNum := -1

	for i, row := range dv.rows {
		if row.kind != rowDiffPair {
			continue
		}

		matchLine := row.lineNum == anchor.lineNum || row.lineNum == fallbackLineNum
		if !matchLine {
			continue
		}

		if bestLineNum == -1 || absInt(i-preferredRow) < absInt(bestLineNum-preferredRow) {
			bestLineNum = i
		}

		rowAnchor := anchorFromRow(row)
		if rowAnchor.hasLeft == anchor.hasLeft && rowAnchor.hasRight == anchor.hasRight {
			if bestPairShape == -1 || absInt(i-preferredRow) < absInt(bestPairShape-preferredRow) {
				bestPairShape = i
			}
		}

		if rowAnchor.hasLeft == anchor.hasLeft &&
			rowAnchor.hasRight == anchor.hasRight &&
			rowAnchor.leftNum == anchor.leftNum &&
			rowAnchor.rightNum == anchor.rightNum {
			if bestExact == -1 || absInt(i-preferredRow) < absInt(bestExact-preferredRow) {
				bestExact = i
			}
		}
	}

	if bestExact != -1 {
		return bestExact
	}
	if bestPairShape != -1 {
		return bestPairShape
	}
	if bestLineNum != -1 {
		return bestLineNum
	}
	return 0
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

	for hi, hunk := range dv.file.Hunks {
		// Add a spacer before each hunk header (except the first)
		if hi > 0 {
			dv.rows = append(dv.rows, diffRow{kind: rowSpacer})
		}
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
				for ci := range dv.comments.Comments {
					c := &dv.comments.Comments[ci]
					if c.File != filename {
						continue
					}
					// Single-line comments attach at c.Line
					// Range comments attach at c.EndLine (last line of range)
					attachLine := c.Line
					if c.EndLine > 0 {
						attachLine = c.EndLine
					}
					if attachLine == lineNum {
						dv.rows = append(dv.rows, diffRow{kind: rowComment, comment: c, lineNum: lineNum})
					}
				}
			}
		}
	}
}

func (dv *diffView) moveCursor(n int) {
	if len(dv.rows) == 0 {
		return
	}
	dv.cursorY += n
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	maxCursor := len(dv.rows) - 1
	if dv.cursorY > maxCursor {
		dv.cursorY = maxCursor
	}
	dv.scrollCursorIntoMargin()
}

// moveCursorTo sets the cursor to an absolute row index.
func (dv *diffView) moveCursorTo(row int) {
	if len(dv.rows) == 0 {
		return
	}
	dv.cursorY = row
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	if dv.cursorY >= len(dv.rows) {
		dv.cursorY = len(dv.rows) - 1
	}
	dv.scrollCursorIntoMargin()
}

// moveCursorToViewportTop moves the cursor to the top visible row.
func (dv *diffView) moveCursorToViewportTop() {
	if len(dv.rows) == 0 {
		return
	}
	dv.cursorY = dv.scrollY
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	if dv.cursorY >= len(dv.rows) {
		dv.cursorY = len(dv.rows) - 1
	}
}

// moveCursorToViewportBottom moves the cursor to the bottom visible row.
func (dv *diffView) moveCursorToViewportBottom() {
	if len(dv.rows) == 0 {
		return
	}
	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		viewportHeight = 1
	}
	dv.cursorY = dv.scrollY + viewportHeight - 1
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	if dv.cursorY >= len(dv.rows) {
		dv.cursorY = len(dv.rows) - 1
	}
}

// findMatches returns row indices whose line content matches the search term (case-insensitive).
func (dv *diffView) findMatches(term string) []int {
	if term == "" {
		return nil
	}
	lower := strings.ToLower(term)
	var matches []int
	for i, row := range dv.rows {
		switch row.kind {
		case rowDiffPair:
			if row.pair == nil {
				continue
			}
			var content string
			if row.pair.Left != nil {
				content = row.pair.Left.Content
			}
			if row.pair.Right != nil {
				content += " " + row.pair.Right.Content
			}
			if strings.Contains(strings.ToLower(content), lower) {
				matches = append(matches, i)
			}
		case rowHunkHeader:
			if strings.Contains(strings.ToLower(row.hunk), lower) {
				matches = append(matches, i)
			}
		case rowComment:
			if row.comment != nil && strings.Contains(strings.ToLower(row.comment.Text), lower) {
				matches = append(matches, i)
			}
		}
	}
	return matches
}

// clickAt maps a Y coordinate (relative to diff content area top) to a row
// and moves the cursor there.
func (dv *diffView) clickAt(y int) {
	row := dv.scrollY + y
	if row < 0 {
		row = 0
	}
	if row >= len(dv.rows) {
		row = len(dv.rows) - 1
	}
	if row >= 0 {
		dv.cursorY = row
	}
}

func (dv *diffView) clampScroll() {
	if dv.scrollY < 0 {
		dv.scrollY = 0
	}
	// Allow scrolling past EOF so cursor line can stay at its screen position,
	// but don't scroll further than the last row being at the top.
	maxScroll := len(dv.rows) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if dv.scrollY > maxScroll {
		dv.scrollY = maxScroll
	}
	// Keep cursor in bounds
	maxCursor := len(dv.rows) - 1
	if maxCursor < 0 {
		maxCursor = 0
	}
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	if dv.cursorY > maxCursor {
		dv.cursorY = maxCursor
	}
	dv.scrollCursorIntoMargin()
}

func (dv *diffView) clampKeepPosition() {
	if dv.scrollY < 0 {
		dv.scrollY = 0
	}
	maxScroll := len(dv.rows) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if dv.scrollY > maxScroll {
		dv.scrollY = maxScroll
	}

	maxCursor := len(dv.rows) - 1
	if maxCursor < 0 {
		maxCursor = 0
	}
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	if dv.cursorY > maxCursor {
		dv.cursorY = maxCursor
	}

	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		return
	}
	minScroll := int(math.Max(0, float64(dv.cursorY-(viewportHeight-1))))
	if dv.scrollY < minScroll {
		dv.scrollY = minScroll
	}
	if dv.scrollY > dv.cursorY {
		dv.scrollY = dv.cursorY
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (dv *diffView) scrollCursorIntoMargin() {
	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		return
	}

	margin := scrollMarginLines
	maxMargin := (viewportHeight - 1) / 2
	if margin > maxMargin {
		margin = maxMargin
	}
	if margin < 0 {
		margin = 0
	}

	// Keep the cursor away from the viewport edges so scrolling starts
	// before the cursor reaches the exact top or bottom line.
	topBoundary := dv.scrollY + margin
	bottomBoundary := dv.scrollY + viewportHeight - margin - 1

	if dv.cursorY < topBoundary {
		dv.scrollY = dv.cursorY - margin
	}
	if dv.cursorY > bottomBoundary {
		dv.scrollY = dv.cursorY - (viewportHeight - margin - 1)
	}

	if dv.scrollY < 0 {
		dv.scrollY = 0
	}
	maxScroll := len(dv.rows) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if dv.scrollY > maxScroll {
		dv.scrollY = maxScroll
	}
}

func (dv *diffView) currentLineNum() int {
	if len(dv.rows) == 0 {
		return 0
	}
	if dv.cursorY >= len(dv.rows) {
		return dv.rows[len(dv.rows)-1].lineNum
	}
	return dv.rows[dv.cursorY].lineNum
}

func (dv *diffView) toggleMode() {
	if dv.mode == viewSideBySide {
		dv.mode = viewUnified
	} else {
		dv.mode = viewSideBySide
	}
}

func (dv *diffView) newCommentTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Enter comment... (enter=newline, ctrl+s=submit, ctrl+g=editor)"
	ta.CharLimit = 2000
	ta.SetWidth(dv.width - 12)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Focus()
	return ta
}

func (dv *diffView) activateComment(file string, lineNum int) {
	ta := dv.newCommentTextarea()
	dv.commentActive = true
	dv.commentFile = file
	dv.commentLineNum = lineNum
	dv.commentEndLine = 0
	dv.commentInput = ta
}

func (dv *diffView) activateRangeComment(file string, startLine, endLine int) {
	ta := dv.newCommentTextarea()
	dv.commentActive = true
	dv.commentFile = file
	dv.commentLineNum = startLine
	dv.commentEndLine = endLine
	dv.commentInput = ta
}

func (dv *diffView) activateEditComment(file string, lineNum int, existingText string) {
	ta := dv.newCommentTextarea()
	ta.SetValue(existingText)
	dv.commentActive = true
	dv.commentEditing = true
	dv.commentFile = file
	dv.commentLineNum = lineNum
	dv.commentInput = ta
}

func (dv *diffView) activateEditRangeComment(file string, lineNum, endLine int, existingText string) {
	ta := dv.newCommentTextarea()
	ta.SetValue(existingText)
	dv.commentActive = true
	dv.commentEditing = true
	dv.commentFile = file
	dv.commentLineNum = lineNum
	dv.commentEndLine = endLine
	dv.commentInput = ta
}

// commentAtCursor returns the comment if the cursor is on a comment row.
func (dv *diffView) commentAtCursor() *review.Comment {
	if dv.cursorY < 0 || dv.cursorY >= len(dv.rows) {
		return nil
	}
	row := dv.rows[dv.cursorY]
	if row.kind == rowComment {
		return row.comment
	}
	return nil
}

func (dv *diffView) deactivateComment() {
	dv.commentActive = false
	dv.commentEditing = false
	dv.commentEndLine = 0
	dv.commentInput.Blur()
}

func (dv *diffView) commentValue() string {
	return dv.commentInput.Value()
}

func (dv *diffView) view() string {
	return dv.viewWithPath("")
}

func (dv *diffView) viewWithPath(filePath string) string {
	if dv.file == nil {
		return diffPanelStyle.Width(dv.width).Height(dv.height).Render("No file selected")
	}

	// Always reserve a line for the file path to keep the layout stable.
	pathText := " "
	if filePath != "" {
		pathText = " " + filePath
	}
	pathWidth := dv.width - 4
	if pathWidth < 1 {
		pathWidth = 1
	}
	pathLine := lipgloss.NewStyle().Foreground(colorDim).Render(truncateToWidth(pathText, pathWidth))

	var content string
	if dv.mode == viewUnified {
		content = dv.renderUnifiedContent()
	} else {
		content = dv.renderSideBySideContent()
	}

	return diffPanelStyle.Width(dv.width).Height(dv.height).Render(pathLine + "\n" + content)
}

func (dv *diffView) renderSideBySideContent() string {
	relGutter := 0
	if dv.showRelative() {
		relGutter = 4 // 3 digits + 1 space
	}
	colWidth := (dv.width - 4 - relGutter) / 2 // account for border + separator + relative gutter
	if colWidth < 20 {
		colWidth = 20
	}

	var lines []string
	viewportHeight := dv.contentViewportHeight()
	end := dv.scrollY + viewportHeight
	if end > len(dv.rows) {
		end = len(dv.rows)
	}

	for i := dv.scrollY; i < end; i++ {
		row := dv.rows[i]
		isCursor := i == dv.cursorY

		relPrefix := ""
		if dv.showRelative() {
			relPrefix = lineNumStyle.Width(3).Render(dv.relativeNumStr(i)) + " "
		}

		switch row.kind {
		case rowSpacer:
			lines = append(lines, "")
		case rowHunkHeader:
			contentW := dv.width - 4 - relGutter
			line := hunkHeaderStyle.Width(contentW).Render(row.hunk)
			if isCursor {
				line = cursorStyle.Width(contentW).Render(row.hunk)
			}
			lines = append(lines, relPrefix+line)
		case rowComment:
			commentText := formatCommentBubble(row.comment)
			lines = append(lines, relPrefix+commentBorderStyle.Width(dv.width-6-relGutter).Render(commentText))
		case rowDiffPair:
			pair := row.pair
			left := dv.renderSide(pair.Left, colWidth, true)
			right := dv.renderSide(pair.Right, colWidth, false)
			sep := lipgloss.NewStyle().Foreground(colorDim).Render("│")
			rendered := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
			if isCursor || dv.isInSelection(i) {
				rendered = withPersistentBg(rendered, bgHex(cursorStyle.GetBackground()))
				visW := lipgloss.Width(rendered)
				cw := dv.width - 4 - relGutter
				if visW < cw {
					rendered += strings.Repeat(" ", cw-visW)
				}
				rendered = cursorStyle.MaxWidth(cw).Render(rendered)
			}
			lines = append(lines, relPrefix+rendered)
		}

		if isCursor && dv.commentActive {
			lines = append(lines, dv.renderInlineInput())
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (dv *diffView) renderSide(line *diff.DiffLine, width int, isLeft bool) string {
	if line == nil {
		return lipgloss.NewStyle().Width(width).Render("")
	}

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

	var combined string
	if dv.showAbsolute() {
		num := line.OldNum
		if !isLeft {
			num = line.NewNum
		}
		numStr := lineNumStyle.Render(fmt.Sprintf("%d", num))
		textWidth := width - 6 // line number width (5) + space (1)
		text := style.Render(truncate(content, textWidth))
		combined = lipgloss.JoinHorizontal(lipgloss.Top, numStr, " ", text)
	} else {
		text := style.Render(truncate(content, width))
		combined = text
	}

	// Pad to exact width manually to avoid lipgloss word-wrapping at hyphens.
	visW := lipgloss.Width(combined)
	if visW < width {
		combined += strings.Repeat(" ", width-visW)
	}

	// Apply subtle background shading for changed lines.
	// Use withPersistentBg so the background shows through syntax highlighting.
	// MaxWidth only (no Width) to prevent lipgloss from wrapping.
	baseStyle := lipgloss.NewStyle().MaxWidth(width)
	switch line.Op {
	case diff.OpDelete:
		combined = withPersistentBg(combined, bgHex(removedBgStyle.GetBackground()))
		baseStyle = baseStyle.Background(removedBgStyle.GetBackground())
	case diff.OpInsert:
		combined = withPersistentBg(combined, bgHex(addedBgStyle.GetBackground()))
		baseStyle = baseStyle.Background(addedBgStyle.GetBackground())
	}
	return baseStyle.Render(combined)
}

func (dv *diffView) renderUnifiedContent() string {
	var lines []string
	viewportHeight := dv.contentViewportHeight()
	end := dv.scrollY + viewportHeight
	if end > len(dv.rows) {
		end = len(dv.rows)
	}

	relGutter := 0
	if dv.showRelative() {
		relGutter = 4 // 3 digits + 1 space
	}
	absGutter := 0
	if dv.showAbsolute() {
		absGutter = 12 // oldNum(5) + space(1) + newNum(5) + space(1)
	}
	contentWidth := dv.width - 4 - relGutter - absGutter // border + gutters

	for i := dv.scrollY; i < end; i++ {
		row := dv.rows[i]
		isCursor := i == dv.cursorY

		relPrefix := ""
		if dv.showRelative() {
			relPrefix = lineNumStyle.Width(3).Render(dv.relativeNumStr(i)) + " "
		}

		switch row.kind {
		case rowSpacer:
			lines = append(lines, "")
		case rowHunkHeader:
			contentW := dv.width - 4 - relGutter
			line := hunkHeaderStyle.Width(contentW).Render(row.hunk)
			if isCursor {
				line = cursorStyle.Width(contentW).Render(row.hunk)
			}
			lines = append(lines, relPrefix+line)
		case rowComment:
			commentText := formatCommentBubble(row.comment)
			lines = append(lines, relPrefix+commentBorderStyle.Width(dv.width-6-relGutter).Render(commentText))
		case rowDiffPair:
			pair := row.pair
			var rendered []string
			// In unified mode, show delete then insert
			if pair.Left != nil && pair.Left.Op == diff.OpDelete {
				rendered = append(rendered, dv.renderUnifiedLine(pair.Left, contentWidth))
			}
			if pair.Right != nil && pair.Right.Op == diff.OpInsert {
				rendered = append(rendered, dv.renderUnifiedLine(pair.Right, contentWidth))
			}
			// For equal lines, just show once
			if pair.Left != nil && pair.Left.Op == diff.OpEqual {
				rendered = append(rendered, dv.renderUnifiedLine(pair.Left, contentWidth))
			}
			for _, r := range rendered {
				if isCursor || dv.isInSelection(i) {
					r = withPersistentBg(r, bgHex(cursorStyle.GetBackground()))
					cw := dv.width - 4 - relGutter
					visW := lipgloss.Width(r)
					if visW < cw {
						r += strings.Repeat(" ", cw-visW)
					}
					r = cursorStyle.MaxWidth(cw).Render(r)
				}
				lines = append(lines, relPrefix+r)
			}
		}

		if isCursor && dv.commentActive {
			lines = append(lines, dv.renderInlineInput())
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (dv *diffView) renderUnifiedLine(line *diff.DiffLine, width int) string {
	prefix := " "
	style := lipgloss.NewStyle()

	switch line.Op {
	case diff.OpDelete:
		prefix = "-"
		style = removedStyle
	case diff.OpInsert:
		prefix = "+"
		style = addedStyle
	}

	content := line.Content
	if dv.highlight != nil && dv.file != nil {
		filename := dv.file.NewName
		if filename == "/dev/null" {
			filename = dv.file.OldName
		}
		content = dv.highlight(filename, content)
	}
	text := style.Render(prefix + truncate(content, width))

	var combined string
	if dv.showAbsolute() {
		oldNum := "     "
		newNum := "     "
		switch line.Op {
		case diff.OpEqual:
			oldNum = fmt.Sprintf("%5d", line.OldNum)
			newNum = fmt.Sprintf("%5d", line.NewNum)
		case diff.OpDelete:
			oldNum = fmt.Sprintf("%5d", line.OldNum)
		case diff.OpInsert:
			newNum = fmt.Sprintf("%5d", line.NewNum)
		}
		numStr := lipgloss.NewStyle().Foreground(colorDim).Render(oldNum + " " + newNum)
		combined = numStr + " " + text
	} else {
		combined = text
	}

	// Pad to exact width manually to avoid lipgloss word-wrapping at hyphens.
	relGutter := 0
	if dv.showRelative() {
		relGutter = 4
	}
	lineWidth := dv.width - 4 - relGutter
	visW := lipgloss.Width(combined)
	if visW < lineWidth {
		combined += strings.Repeat(" ", lineWidth-visW)
	}

	// Apply subtle background shading for changed lines.
	// Use withPersistentBg so the background shows through syntax highlighting.
	// MaxWidth only (no Width) to prevent lipgloss from wrapping.
	baseStyle := lipgloss.NewStyle().MaxWidth(lineWidth)
	switch line.Op {
	case diff.OpDelete:
		combined = withPersistentBg(combined, bgHex(removedBgStyle.GetBackground()))
		baseStyle = baseStyle.Background(removedBgStyle.GetBackground())
	case diff.OpInsert:
		combined = withPersistentBg(combined, bgHex(addedBgStyle.GetBackground()))
		baseStyle = baseStyle.Background(addedBgStyle.GetBackground())
	}
	return baseStyle.Render(combined)
}

func (dv *diffView) renderInlineInput() string {
	var labelText string
	if dv.commentEndLine > 0 {
		labelText = fmt.Sprintf(" Comment on lines %d-%d: ", dv.commentLineNum, dv.commentEndLine)
	} else {
		labelText = fmt.Sprintf(" Comment on line %d: ", dv.commentLineNum)
	}
	label := commentPromptStyle.Render(labelText)
	input := dv.commentInput.View()
	content := label + "\n" + input
	return commentBorderStyle.Width(dv.width - 6).Render(content)
}

func (dv *diffView) isInSelection(rowIdx int) bool {
	if !dv.selectionActive {
		return false
	}
	lo, hi := dv.selectionAnchor, dv.cursorY
	if lo > hi {
		lo, hi = hi, lo
	}
	return rowIdx >= lo && rowIdx <= hi
}

// selectionLineRange returns the (startLine, endLine) for the current visual selection.
func (dv *diffView) selectionLineRange() (int, int) {
	lo, hi := dv.selectionAnchor, dv.cursorY
	if lo > hi {
		lo, hi = hi, lo
	}
	startLine := 0
	endLine := 0
	for i := lo; i <= hi; i++ {
		if i < 0 || i >= len(dv.rows) {
			continue
		}
		ln := dv.rows[i].lineNum
		if ln == 0 {
			continue
		}
		if startLine == 0 || ln < startLine {
			startLine = ln
		}
		if ln > endLine {
			endLine = ln
		}
	}
	return startLine, endLine
}

func formatCommentBubble(c *review.Comment) string {
	if c.EndLine > 0 {
		return fmt.Sprintf("💬 [lines %d-%d] %s", c.Line, c.EndLine, c.Text)
	}
	return "💬 " + c.Text
}

func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	// ANSI-aware truncation: measure visible width ignoring escape sequences
	visibleWidth := lipgloss.Width(s)
	if visibleWidth <= maxWidth {
		return s
	}
	// Strip ANSI, truncate, then we lose highlighting on truncated lines
	// but at least we don't produce garbled output
	stripped := stripAnsi(s)
	runes := []rune(stripped)
	if len(runes) > maxWidth-1 {
		return string(runes[:maxWidth-1]) + "…"
	}
	return stripped
}

func stripAnsi(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until we find the terminating letter
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				j++ // skip the terminating letter
			}
			i = j
		} else {
			out = append(out, s[i])
			i++
		}
	}
	return string(out)
}

// withPersistentBg injects a background color into an ANSI-formatted string
// so it persists through escape sequence resets. This is necessary because
// Chroma's syntax highlighter inserts SGR sequences (resets, color changes)
// that clear any background set by an outer lipgloss style.
//
// The approach: re-inject the background color after every SGR sequence
// (any CSI sequence terminated by 'm'), so no matter what Chroma does
// (full reset, shorthand reset, combined reset+color), the background
// is always re-established.
func withPersistentBg(s string, hexColor string) string {
	hexColor = strings.TrimPrefix(hexColor, "#")
	if len(hexColor) != 6 {
		return s
	}
	r, _ := strconv.ParseInt(hexColor[0:2], 16, 0)
	g, _ := strconv.ParseInt(hexColor[2:4], 16, 0)
	b, _ := strconv.ParseInt(hexColor[4:6], 16, 0)
	bgCode := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)

	var result strings.Builder
	result.Grow(len(s) + len(bgCode)*8)
	result.WriteString(bgCode)
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Find end of CSI sequence (terminated by a letter)
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				terminator := s[j]
				j++
				result.WriteString(s[i:j])
				if terminator == 'm' {
					// Re-inject background after every SGR sequence
					result.WriteString(bgCode)
				}
				i = j
			} else {
				result.WriteByte(s[i])
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// bgHex extracts the hex color string from a lipgloss.TerminalColor.
func bgHex(c lipgloss.TerminalColor) string {
	if c == nil {
		return ""
	}
	if lc, ok := c.(lipgloss.Color); ok {
		return string(lc)
	}
	return ""
}

func (dv *diffView) contentViewportHeight() int {
	if dv.height <= 1 {
		return 0
	}
	return dv.height - 1
}
