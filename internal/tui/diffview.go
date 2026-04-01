package tui

import (
	"fmt"
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
	// Logical row ordinals used for relative numbering; only diff rows advance.
	rowOrdinals []int
	// Maps currently rendered screen lines to logical row indices.
	visibleRowMap []int
	// Maps currently rendered screen lines to comment rows (if any).
	visibleCommentMap []*review.Comment
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
	// Per-file render caches to avoid recomputing expensive highlighting/wrapping
	// on every cursor move redraw.
	highlightCache map[string]string
	wrapCache      map[string][]string
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
	return diffView{
		mode:           viewSideBySide,
		lineNums:       lineNumBoth,
		highlightCache: make(map[string]string),
		wrapCache:      make(map[string][]string),
	}
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

// isSelectableRow returns true if the row kind is one the cursor should land on.
func isSelectableRow(kind diffRowKind) bool {
	return kind == rowDiffPair || kind == rowComment
}

// snapToSelectable finds the nearest selectable row starting from pos.
// dir should be +1 (forward) or -1 (backward). If no selectable row exists
// in that direction, it searches the opposite direction. Returns pos unchanged
// if no rows exist.
func (dv *diffView) snapToSelectable(pos, dir int) int {
	if len(dv.rows) == 0 {
		return pos
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= len(dv.rows) {
		pos = len(dv.rows) - 1
	}
	if isSelectableRow(dv.rows[pos].kind) {
		return pos
	}
	// Search in preferred direction.
	for i := pos + dir; i >= 0 && i < len(dv.rows); i += dir {
		if isSelectableRow(dv.rows[i].kind) {
			return i
		}
	}
	// Search opposite direction.
	for i := pos - dir; i >= 0 && i < len(dv.rows); i -= dir {
		if isSelectableRow(dv.rows[i].kind) {
			return i
		}
	}
	return pos
}

// setCursor sets the cursor to the given row, snapping to the nearest
// selectable row in the given direction, and scrolls to keep it visible.
func (dv *diffView) setCursor(row, dir int) {
	if len(dv.rows) == 0 {
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= len(dv.rows) {
		row = len(dv.rows) - 1
	}
	dv.cursorY = dv.snapToSelectable(row, dir)
	dv.scrollCursorIntoMargin()
}

// setCursorNoScroll sets the cursor to the given row, snapping to the nearest
// selectable row in the given direction, without adjusting scroll position.
func (dv *diffView) setCursorNoScroll(row, dir int) {
	if len(dv.rows) == 0 {
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= len(dv.rows) {
		row = len(dv.rows) - 1
	}
	dv.cursorY = dv.snapToSelectable(row, dir)
}

// relativeNumStr returns a 3-char right-aligned relative row number string.
// The cursor row shows "  0", spacers and hunk headers show "   ", others show distance.
func (dv *diffView) relativeNumStr(rowIdx int) string {
	if rowIdx >= 0 && rowIdx < len(dv.rows) && !isSelectableRow(dv.rows[rowIdx].kind) {
		return "   "
	}
	if rowIdx == dv.cursorY {
		return "  0"
	}
	if rowIdx < 0 || rowIdx >= len(dv.rowOrdinals) || dv.cursorY < 0 || dv.cursorY >= len(dv.rowOrdinals) {
		dist := rowIdx - dv.cursorY
		if dist < 0 {
			dist = -dist
		}
		return fmt.Sprintf("%3d", dist)
	}
	dist := dv.rowOrdinals[rowIdx] - dv.rowOrdinals[dv.cursorY]
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
	dv.resetRenderCaches()
	dv.buildRows()
	dv.setCursorNoScroll(0, +1)
}

// setFileKeepPosition switches the file data but keeps the cursor on the same
// line number at the same screen row. Used when toggling expand.
func (dv *diffView) setFileKeepPosition(f *diff.FileDiff, rev *review.Review) {
	// Remember current line number and its screen offset (in screen lines, not logical rows)
	oldLineNum := 0
	anchor := diffRowAnchor{}
	if dv.cursorY < len(dv.rows) {
		oldRow := dv.rows[dv.cursorY]
		oldLineNum = oldRow.lineNum
		anchor = anchorFromRow(oldRow)
	}
	screenOffset := dv.screenLinesInRange(dv.scrollY, dv.cursorY)

	dv.file = f
	dv.comments = rev
	dv.resetRenderCaches()
	dv.buildRows()

	// Find the same row identity in the new rows.
	newCursor := dv.findBestAnchorRow(anchor, oldLineNum, dv.cursorY)

	dv.cursorY = newCursor
	// Find scrollY such that cursor has the same screen-line offset from viewport top.
	accum := 0
	sy := newCursor
	for sy > 0 && accum < screenOffset {
		sy--
		accum += dv.screenLinesForRow(sy)
	}
	dv.scrollY = sy
	dv.clampKeepPosition()
}

func (dv *diffView) resetRenderCaches() {
	if len(dv.highlightCache) > 0 {
		dv.highlightCache = make(map[string]string, len(dv.highlightCache))
	}
	if len(dv.wrapCache) > 0 {
		dv.wrapCache = make(map[string][]string, len(dv.wrapCache))
	}
}

func (dv *diffView) wrappedChunks(content string, width int) []string {
	key := strconv.Itoa(width) + "\x00" + content
	if cached, ok := dv.wrapCache[key]; ok {
		return cached
	}
	chunks := wrapToWidth(content, width)
	dv.wrapCache[key] = chunks
	return chunks
}

func (dv *diffView) highlightChunk(filename, chunk string) string {
	if dv.highlight == nil || filename == "" || chunk == "" {
		return chunk
	}
	key := filename + "\x00" + chunk
	if cached, ok := dv.highlightCache[key]; ok {
		return cached
	}
	out := dv.highlight(filename, chunk)
	dv.highlightCache[key] = out
	return out
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
	dv.rowOrdinals = nil
	dv.visibleRowMap = nil
	dv.visibleCommentMap = nil
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

	ordinal := 0
	dv.rowOrdinals = make([]int, len(dv.rows))
	for i, row := range dv.rows {
		if row.kind == rowDiffPair {
			ordinal++
		}
		dv.rowOrdinals[i] = ordinal
	}
}

func (dv *diffView) moveCursor(n int) {
	if len(dv.rows) == 0 {
		return
	}
	dir := +1
	if n < 0 {
		dir = -1
	}
	dv.setCursor(dv.cursorY+n, dir)
}

// moveCursorByOrdinalDelta moves relative to the diff-row ordinals used by the
// relative-number gutter. This skips hunk headers and comment rows when
// applying the delta, while still preserving normal cursor autoscroll.
func (dv *diffView) moveCursorByOrdinalDelta(delta int) {
	if len(dv.rows) == 0 || delta == 0 {
		return
	}

	currentOrdinal := dv.nearestOrdinal(delta > 0)
	if currentOrdinal == 0 {
		return
	}
	dv.moveCursorToOrdinal(currentOrdinal + delta)
}

// scrollByRows scrolls the viewport by row count while keeping the cursor
// anchored to the same on-screen position where possible. Uses screen-line-aware
// measurement so soft-wrapped rows are handled correctly.
func (dv *diffView) scrollByRows(n int) {
	if len(dv.rows) == 0 || n == 0 {
		return
	}

	// Remember cursor's screen-line offset from viewport top.
	cursorScreenOffset := dv.screenLinesInRange(dv.scrollY, dv.cursorY)

	maxScroll := len(dv.rows) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}

	dv.scrollY += n
	if dv.scrollY < 0 {
		dv.scrollY = 0
	}
	if dv.scrollY > maxScroll {
		dv.scrollY = maxScroll
	}

	// Restore cursor to the same screen-line offset from new scrollY.
	accum := 0
	dv.cursorY = dv.scrollY
	for i := dv.scrollY; i < len(dv.rows); i++ {
		if accum >= cursorScreenOffset {
			dv.cursorY = i
			break
		}
		dv.cursorY = i
		accum += dv.screenLinesForRow(i)
	}

	// Clamp cursor to visible viewport.
	if dv.cursorY < dv.scrollY {
		dv.cursorY = dv.scrollY
	}
	last := dv.lastVisibleRow()
	if dv.cursorY > last {
		dv.cursorY = last
	}
	dir := +1
	if n < 0 {
		dir = -1
	}
	dv.setCursorNoScroll(dv.cursorY, dir)
}

// windowScrollByScreenLines scrolls the viewport by a screen-line count
// without anchoring movement to the cursor position. This keeps forward and
// reverse paging symmetric even when wrapped rows change the logical-row
// count visible in the viewport.
func (dv *diffView) windowScrollByScreenLines(n int) {
	if len(dv.rows) == 0 || n == 0 {
		return
	}

	origScrollY := dv.scrollY
	cursorScreenOffset := dv.screenLinesInRange(dv.scrollY, dv.cursorY)

	if n > 0 {
		accum := 0
		next := dv.scrollY
		for next < len(dv.rows) && accum < n {
			accum += dv.screenLinesForRow(next)
			next++
		}
		dv.scrollY = next
	} else {
		target := -n
		accum := 0
		prev := dv.scrollY
		for prev > 0 && accum < target {
			prev--
			accum += dv.screenLinesForRow(prev)
		}
		dv.scrollY = prev
	}

	if dv.scrollY < 0 {
		dv.scrollY = 0
	}
	maxScroll := dv.maxWindowScrollY()
	if dv.scrollY > maxScroll {
		dv.scrollY = maxScroll
	}
	if dv.scrollY == origScrollY {
		return
	}

	last := dv.lastVisibleRow()
	target := dv.rowAtScreenOffset(dv.scrollY, cursorScreenOffset)
	if target < dv.scrollY {
		target = dv.scrollY
	}
	if target > last {
		target = last
	}
	dir := +1
	if n < 0 {
		dir = -1
	}
	dv.cursorY = dv.snapToSelectableInRange(target, dir, dv.scrollY, last)
}

// maxWindowScrollY returns the last top row that can still show a full
// viewport of content. Window paging should clamp here so paging up from EOF
// uses the viewport height instead of a shortened tail.
func (dv *diffView) maxWindowScrollY() int {
	if len(dv.rows) == 0 {
		return 0
	}
	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		return 0
	}

	accum := 0
	top := len(dv.rows)
	for top > 0 && accum < viewportHeight {
		top--
		accum += dv.screenLinesForRow(top)
	}
	if accum < viewportHeight {
		return 0
	}
	return top
}

func (dv *diffView) rowAtScreenOffset(startRow, screenOffset int) int {
	if len(dv.rows) == 0 {
		return 0
	}
	if startRow < 0 {
		startRow = 0
	}
	if startRow >= len(dv.rows) {
		startRow = len(dv.rows) - 1
	}
	if screenOffset <= 0 {
		return startRow
	}

	accum := 0
	for i := startRow; i < len(dv.rows); i++ {
		rowH := dv.screenLinesForRow(i)
		if accum+rowH > screenOffset {
			return i
		}
		accum += rowH
	}
	return len(dv.rows) - 1
}

func (dv *diffView) snapToSelectableInRange(pos, dir, minRow, maxRow int) int {
	if len(dv.rows) == 0 {
		return pos
	}
	if minRow < 0 {
		minRow = 0
	}
	if maxRow >= len(dv.rows) {
		maxRow = len(dv.rows) - 1
	}
	if maxRow < minRow {
		return dv.snapToSelectable(pos, dir)
	}
	if pos < minRow {
		pos = minRow
	}
	if pos > maxRow {
		pos = maxRow
	}
	if isSelectableRow(dv.rows[pos].kind) {
		return pos
	}
	for i := pos + dir; i >= minRow && i <= maxRow; i += dir {
		if isSelectableRow(dv.rows[i].kind) {
			return i
		}
	}
	for i := pos - dir; i >= minRow && i <= maxRow; i -= dir {
		if isSelectableRow(dv.rows[i].kind) {
			return i
		}
	}
	return pos
}

// moveCursorTo sets the cursor to an absolute row index.
func (dv *diffView) moveCursorTo(row int) {
	dv.setCursor(row, +1)
}

// moveCursorToViewportTop moves the cursor to the top visible row.
func (dv *diffView) moveCursorToViewportTop() {
	if len(dv.rows) == 0 {
		return
	}
	if len(dv.visibleRowMap) > 0 {
		dv.setCursorNoScroll(dv.visibleRowMap[0], +1)
		return
	}
	dv.setCursorNoScroll(dv.scrollY, +1)
}

// moveCursorToViewportBottom moves the cursor to the bottom visible row.
func (dv *diffView) moveCursorToViewportBottom() {
	if len(dv.rows) == 0 {
		return
	}
	if n := len(dv.visibleRowMap); n > 0 {
		dv.setCursorNoScroll(dv.visibleRowMap[n-1], -1)
		return
	}
	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		viewportHeight = 1
	}
	dv.setCursorNoScroll(dv.scrollY+viewportHeight-1, -1)
}

// fileDiffContains reports whether any hunk line in f contains term.
func fileDiffContains(f *diff.FileDiff, term string) bool {
	for i := range f.Hunks {
		for _, dl := range f.Hunks[i].Lines {
			if strings.Contains(dl.Content, term) {
				return true
			}
		}
	}
	return false
}

// findMatches returns row indices whose line content matches the search term (case-sensitive).
func (dv *diffView) findMatches(term string) []int {
	if term == "" {
		return nil
	}
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
			if strings.Contains(content, term) {
				matches = append(matches, i)
			}
		case rowHunkHeader:
			if strings.Contains(row.hunk, term) {
				matches = append(matches, i)
			}
		case rowComment:
			if row.comment != nil && strings.Contains(row.comment.Text, term) {
				matches = append(matches, i)
			}
		}
	}
	return matches
}

// clickAt maps a Y coordinate (relative to diff content area top) to a row
// and moves the cursor there.
func (dv *diffView) clickAt(y int) {
	if y >= 0 && y < len(dv.visibleRowMap) {
		dv.setCursorNoScroll(dv.visibleRowMap[y], +1)
		return
	}
	dv.setCursorNoScroll(dv.scrollY+y, +1)
}

func (dv *diffView) commentAtScreenY(y int) *review.Comment {
	if y < 0 || y >= len(dv.visibleCommentMap) {
		return nil
	}
	return dv.visibleCommentMap[y]
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
	// Keep cursor in bounds and snap to a selectable row without reapplying
	// normal scroll-margin behavior during layout clamps.
	dv.setCursorNoScroll(dv.cursorY, +1)

	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		return
	}
	if dv.cursorY < dv.scrollY {
		dv.cursorY = dv.scrollY
	}
	last := dv.lastVisibleRow()
	if dv.cursorY > last {
		dv.cursorY = last
	}
	dv.setCursorNoScroll(dv.cursorY, +1)
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

	dv.setCursorNoScroll(dv.cursorY, +1)

	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 {
		return
	}

	// Ensure cursor is not above viewport.
	if dv.scrollY > dv.cursorY {
		dv.scrollY = dv.cursorY
	}
	// Ensure cursor fits in viewport using screen-line-aware measurement.
	for dv.scrollY < dv.cursorY && dv.screenLinesInRange(dv.scrollY, dv.cursorY+1) > viewportHeight {
		dv.scrollY++
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

	// Measure cursor position in screen lines, accounting for soft-wrap.
	var cursorOffset int
	if dv.cursorY >= dv.scrollY {
		cursorOffset = dv.screenLinesInRange(dv.scrollY, dv.cursorY)
	} else {
		cursorOffset = -dv.screenLinesInRange(dv.cursorY, dv.scrollY)
	}
	cursorHeight := dv.screenLinesForRow(dv.cursorY)

	if cursorOffset < margin {
		// Cursor too close to top (or above viewport): scroll up so cursor
		// has at least `margin` screen lines above it.
		accum := 0
		sy := dv.cursorY
		for sy > 0 && accum < margin {
			sy--
			accum += dv.screenLinesForRow(sy)
		}
		dv.scrollY = sy
	} else if cursorOffset+cursorHeight > viewportHeight-margin {
		// Cursor too close to bottom: scroll down so cursor's bottom edge
		// has at least `margin` screen lines below it.
		target := viewportHeight - margin
		accum := cursorHeight
		sy := dv.cursorY
		for sy > 0 {
			sy--
			rowH := dv.screenLinesForRow(sy)
			accum += rowH
			if accum > target {
				sy++ // went one row too far
				break
			}
			if accum == target {
				break
			}
		}
		dv.scrollY = sy
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

func (dv *diffView) nearestOrdinal(forward bool) int {
	if len(dv.rows) == 0 {
		return 0
	}
	if dv.cursorY < 0 {
		dv.cursorY = 0
	}
	if dv.cursorY >= len(dv.rows) {
		dv.cursorY = len(dv.rows) - 1
	}
	if ordinal := dv.rowOrdinalAt(dv.cursorY); ordinal > 0 {
		return ordinal
	}
	if forward {
		for i := dv.cursorY + 1; i < len(dv.rows); i++ {
			if ordinal := dv.rowOrdinalAt(i); ordinal > 0 {
				return ordinal
			}
		}
	}
	for i := dv.cursorY - 1; i >= 0; i-- {
		if ordinal := dv.rowOrdinalAt(i); ordinal > 0 {
			return ordinal
		}
	}
	if !forward {
		for i := dv.cursorY + 1; i < len(dv.rows); i++ {
			if ordinal := dv.rowOrdinalAt(i); ordinal > 0 {
				return ordinal
			}
		}
	}
	return 0
}

func (dv *diffView) rowOrdinalAt(row int) int {
	if row < 0 || row >= len(dv.rows) || row >= len(dv.rowOrdinals) {
		return 0
	}
	if dv.rows[row].kind != rowDiffPair {
		return 0
	}
	return dv.rowOrdinals[row]
}

func (dv *diffView) moveCursorToOrdinal(ordinal int) {
	if len(dv.rows) == 0 {
		return
	}
	if ordinal < 1 {
		ordinal = 1
	}

	lastDiffRow := -1
	for i := range dv.rows {
		if dv.rows[i].kind != rowDiffPair {
			continue
		}
		lastDiffRow = i
		if dv.rowOrdinals[i] >= ordinal {
			dv.moveCursorTo(i)
			return
		}
	}

	if lastDiffRow >= 0 {
		dv.moveCursorTo(lastDiffRow)
	}
}

// moveCursorToLineNum jumps to the first visible row associated with the given
// source line number. If that exact line is not visible in the current diff, it
// falls forward to the next visible source line; if none exists, it falls back
// to the last visible source line row.
func (dv *diffView) moveCursorToLineNum(lineNum int) {
	if len(dv.rows) == 0 {
		return
	}
	if lineNum < 1 {
		lineNum = 1
	}

	lastLineRow := -1
	for i, row := range dv.rows {
		if row.lineNum == 0 {
			continue
		}
		lastLineRow = i
		if row.lineNum >= lineNum {
			dv.moveCursorTo(i)
			return
		}
	}

	if lastLineRow >= 0 {
		dv.moveCursorTo(lastLineRow)
	}
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
	ta.Placeholder = "Enter comment..."
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
	dv.selectionActive = false
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
	colWidth := (dv.width - 4 - (2 * relGutter)) / 2 // border + separator + relative gutters (LHS + RHS)
	if colWidth < 20 {
		colWidth = 20
	}

	var lines []string
	var rowMap []int
	var commentMap []*review.Comment
	viewportHeight := dv.contentViewportHeight()
	for i := dv.scrollY; i < len(dv.rows) && len(lines) < viewportHeight; i++ {
		row := dv.rows[i]
		isCursor := i == dv.cursorY

		relPrefix := ""
		if dv.showRelative() {
			relPrefix = relativeNumStyle.Render(dv.relativeNumStr(i)) + " "
		}

		var rowLines []string
		switch row.kind {
		case rowSpacer:
			rowLines = append(rowLines, "")
		case rowHunkHeader:
			contentW := dv.width - 4 - relGutter
			for _, seg := range dv.wrappedChunks(row.hunk, contentW) {
				line := hunkHeaderStyle.Width(contentW).Render(seg)
				if isCursor {
					line = cursorStyle.Width(contentW).Render(seg)
				}
				rowLines = append(rowLines, relPrefix+line)
			}
		case rowComment:
			commentText := formatCommentBubble(row.comment)
			commentBoxW := dv.width - 6 - relGutter
			if commentBoxW < 1 {
				commentBoxW = 1
			}
			commentInnerW := commentBoxW - 4 // rounded border + horizontal padding
			if commentInnerW < 1 {
				commentInnerW = 1
			}
			commentText = strings.Join(wrapMultilineToWidth(commentText, commentInnerW), "\n")
			commentLines := splitRenderedLines(commentBorderStyle.Width(commentBoxW).Render(commentText))
			rowLines = append(rowLines, prefixBlankGutter(commentLines, relPrefix)...)
		case rowDiffPair:
			pair := row.pair
			leftLines := dv.renderSideWrapped(pair.Left, colWidth, true)
			rightLines := dv.renderSideWrapped(pair.Right, colWidth, false)
			n := len(leftLines)
			if len(rightLines) > n {
				n = len(rightLines)
			}
			sep := lipgloss.NewStyle().Foreground(colorDim).Render("│")
			for si := 0; si < n; si++ {
				left := blankWidth(colWidth)
				right := blankWidth(colWidth)
				if si < len(leftLines) {
					left = leftLines[si]
				}
				if si < len(rightLines) {
					right = rightLines[si]
				}
				rightRelPrefix := ""
				if dv.showRelative() {
					rightRelPrefix = relativeNumStyle.Render(dv.relativeNumStr(i)) + " "
				}
				rendered := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, rightRelPrefix+right)
				cw := dv.width - 4 - relGutter - relGutter
				if isCursor || dv.isInSelection(i) {
					rendered = withPersistentBg(rendered, bgHex(cursorStyle.GetBackground()))
					visW := lipgloss.Width(rendered)
					if visW < cw {
						rendered += strings.Repeat(" ", cw-visW)
					}
					rendered = cursorStyle.MaxWidth(cw).Render(rendered)
				} else if dv.isInCommentRange(i) {
					rendered = withPersistentBg(rendered, bgHex(commentRangeBgStyle.GetBackground()))
					visW := lipgloss.Width(rendered)
					if visW < cw {
						rendered += strings.Repeat(" ", cw-visW)
					}
					rendered = commentRangeBgStyle.MaxWidth(cw).Render(rendered)
				}
				rowLines = append(rowLines, relPrefix+rendered)
			}
		}

		for _, rl := range rowLines {
			if len(lines) >= viewportHeight {
				break
			}
			lines = append(lines, rl)
			rowMap = append(rowMap, i)
			if row.kind == rowComment {
				commentMap = append(commentMap, row.comment)
			} else {
				commentMap = append(commentMap, nil)
			}
		}

		if isCursor && dv.commentActive {
			inlineLines := prefixBlankGutter(splitRenderedLines(dv.renderInlineInput()), relPrefix)
			for _, il := range inlineLines {
				if len(lines) >= viewportHeight {
					break
				}
				lines = append(lines, il)
				rowMap = append(rowMap, i)
				commentMap = append(commentMap, nil)
			}
		}
	}
	dv.visibleRowMap = rowMap
	dv.visibleCommentMap = commentMap

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (dv *diffView) renderSideWrapped(line *diff.DiffLine, width int, isLeft bool) []string {
	if line == nil {
		return []string{lipgloss.NewStyle().Width(width).Render("")}
	}

	content := line.Content

	var style lipgloss.Style
	switch line.Op {
	case diff.OpDelete:
		style = removedStyle
	case diff.OpInsert:
		style = addedStyle
	default:
		style = lipgloss.NewStyle()
	}

	filename := ""
	if dv.highlight != nil && dv.file != nil {
		filename = dv.file.NewName
		if filename == "/dev/null" {
			filename = dv.file.OldName
		}
	}

	gutterWidth := 0
	if dv.showAbsolute() {
		gutterWidth = 6
	}
	textWidth := width - gutterWidth
	if textWidth < 1 {
		textWidth = 1
	}
	chunks := dv.wrappedChunks(content, textWidth)

	var out []string
	for i, chunk := range chunks {
		textChunk := chunk
		textChunk = dv.highlightChunk(filename, chunk)

		var combined string
		if dv.showAbsolute() {
			numStr := lineNumStyle.Render(sideLineNum(line, isLeft, i))
			combined = lipgloss.JoinHorizontal(lipgloss.Top, numStr, " ", style.Render(textChunk))
		} else {
			combined = style.Render(textChunk)
		}

		visW := lipgloss.Width(combined)
		if visW < width {
			combined += strings.Repeat(" ", width-visW)
		}

		baseStyle := lipgloss.NewStyle().MaxWidth(width)
		switch line.Op {
		case diff.OpDelete:
			combined = withPersistentBg(combined, bgHex(removedBgStyle.GetBackground()))
			baseStyle = baseStyle.Background(removedBgStyle.GetBackground())
		case diff.OpInsert:
			combined = withPersistentBg(combined, bgHex(addedBgStyle.GetBackground()))
			baseStyle = baseStyle.Background(addedBgStyle.GetBackground())
		}
		out = append(out, baseStyle.Render(combined))
	}
	return out
}

func sideLineNum(line *diff.DiffLine, isLeft bool, chunkIdx int) string {
	// Show absolute number only on the first wrapped segment.
	if chunkIdx > 0 {
		return ""
	}
	n := line.OldNum
	if !isLeft {
		n = line.NewNum
	}
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

func (dv *diffView) renderUnifiedContent() string {
	var lines []string
	var rowMap []int
	var commentMap []*review.Comment
	viewportHeight := dv.contentViewportHeight()

	relGutter := 0
	if dv.showRelative() {
		relGutter = 4 // 3 digits + 1 space
	}
	absGutter := 0
	if dv.showAbsolute() {
		absGutter = 12 // oldNum(5) + space(1) + newNum(5) + space(1)
	}
	contentWidth := dv.width - 4 - relGutter - absGutter // border + gutters

	for i := dv.scrollY; i < len(dv.rows) && len(lines) < viewportHeight; i++ {
		row := dv.rows[i]
		isCursor := i == dv.cursorY

		relPrefix := ""
		if dv.showRelative() {
			relPrefix = relativeNumStyle.Render(dv.relativeNumStr(i)) + " "
		}

		var rowLines []string
		switch row.kind {
		case rowSpacer:
			rowLines = append(rowLines, "")
		case rowHunkHeader:
			contentW := dv.width - 4 - relGutter
			for _, seg := range dv.wrappedChunks(row.hunk, contentW) {
				line := hunkHeaderStyle.Width(contentW).Render(seg)
				if isCursor {
					line = cursorStyle.Width(contentW).Render(seg)
				}
				rowLines = append(rowLines, relPrefix+line)
			}
		case rowComment:
			commentText := formatCommentBubble(row.comment)
			commentBoxW := dv.width - 6 - relGutter
			if commentBoxW < 1 {
				commentBoxW = 1
			}
			commentInnerW := commentBoxW - 4 // rounded border + horizontal padding
			if commentInnerW < 1 {
				commentInnerW = 1
			}
			commentText = strings.Join(wrapMultilineToWidth(commentText, commentInnerW), "\n")
			commentLines := splitRenderedLines(commentBorderStyle.Width(commentBoxW).Render(commentText))
			rowLines = append(rowLines, prefixBlankGutter(commentLines, relPrefix)...)
		case rowDiffPair:
			pair := row.pair
			var rendered []string
			// In unified mode, show delete then insert
			if pair.Left != nil && pair.Left.Op == diff.OpDelete {
				rendered = append(rendered, dv.renderUnifiedLineWrapped(pair.Left, contentWidth)...)
			}
			if pair.Right != nil && pair.Right.Op == diff.OpInsert {
				rendered = append(rendered, dv.renderUnifiedLineWrapped(pair.Right, contentWidth)...)
			}
			// For equal lines, just show once
			if pair.Left != nil && pair.Left.Op == diff.OpEqual {
				rendered = append(rendered, dv.renderUnifiedLineWrapped(pair.Left, contentWidth)...)
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
				} else if dv.isInCommentRange(i) {
					r = withPersistentBg(r, bgHex(commentRangeBgStyle.GetBackground()))
					cw := dv.width - 4 - relGutter
					visW := lipgloss.Width(r)
					if visW < cw {
						r += strings.Repeat(" ", cw-visW)
					}
					r = commentRangeBgStyle.MaxWidth(cw).Render(r)
				}
				rowLines = append(rowLines, relPrefix+r)
			}
		}

		for _, rl := range rowLines {
			if len(lines) >= viewportHeight {
				break
			}
			lines = append(lines, rl)
			rowMap = append(rowMap, i)
			if row.kind == rowComment {
				commentMap = append(commentMap, row.comment)
			} else {
				commentMap = append(commentMap, nil)
			}
		}

		if isCursor && dv.commentActive {
			inlineLines := prefixBlankGutter(splitRenderedLines(dv.renderInlineInput()), relPrefix)
			for _, il := range inlineLines {
				if len(lines) >= viewportHeight {
					break
				}
				lines = append(lines, il)
				rowMap = append(rowMap, i)
				commentMap = append(commentMap, nil)
			}
		}
	}
	dv.visibleRowMap = rowMap
	dv.visibleCommentMap = commentMap

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (dv *diffView) renderUnifiedLineWrapped(line *diff.DiffLine, width int) []string {
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
	textWidth := width - 1
	if textWidth < 1 {
		textWidth = 1
	}
	chunks := dv.wrappedChunks(content, textWidth)
	filename := ""
	if dv.highlight != nil && dv.file != nil {
		filename = dv.file.NewName
		if filename == "/dev/null" {
			filename = dv.file.OldName
		}
	}

	var out []string
	for i, chunk := range chunks {
		textChunk := chunk
		textChunk = dv.highlightChunk(filename, chunk)
		text := style.Render(prefix + textChunk)

		var combined string
		if dv.showAbsolute() {
			oldNum := "     "
			newNum := "     "
			if i == 0 {
				switch line.Op {
				case diff.OpEqual:
					oldNum = fmt.Sprintf("%5d", line.OldNum)
					newNum = fmt.Sprintf("%5d", line.NewNum)
				case diff.OpDelete:
					oldNum = fmt.Sprintf("%5d", line.OldNum)
				case diff.OpInsert:
					newNum = fmt.Sprintf("%5d", line.NewNum)
				}
			}
			numStr := lipgloss.NewStyle().Foreground(colorDim).Render(oldNum + " " + newNum)
			combined = numStr + " " + text
		} else {
			combined = text
		}

		relGutter := 0
		if dv.showRelative() {
			relGutter = 4
		}
		lineWidth := dv.width - 4 - relGutter
		visW := lipgloss.Width(combined)
		if visW < lineWidth {
			combined += strings.Repeat(" ", lineWidth-visW)
		}

		baseStyle := lipgloss.NewStyle().MaxWidth(lineWidth)
		switch line.Op {
		case diff.OpDelete:
			combined = withPersistentBg(combined, bgHex(removedBgStyle.GetBackground()))
			baseStyle = baseStyle.Background(removedBgStyle.GetBackground())
		case diff.OpInsert:
			combined = withPersistentBg(combined, bgHex(addedBgStyle.GetBackground()))
			baseStyle = baseStyle.Background(addedBgStyle.GetBackground())
		}
		out = append(out, baseStyle.Render(combined))
		prefix = " "
	}
	return out
}

func (dv *diffView) renderInlineInput() string {
	var labelText string
	if dv.commentEndLine > 0 {
		labelText = fmt.Sprintf(" Comment on lines %d-%d: ", dv.commentLineNum, dv.commentEndLine)
	} else {
		labelText = fmt.Sprintf(" Comment on line %d: ", dv.commentLineNum)
	}
	label := commentPromptStyle.Render(labelText)
	hint := lipgloss.NewStyle().Foreground(colorDim).Render("ctrl+s submit | enter newline | ctrl+g editor | esc cancel")
	input := dv.commentInput.View()
	content := label + hint + "\n" + input
	return commentBorderStyle.Width(dv.width - 6).Render(content)
}

// isInCommentRange returns true if the given row's line number falls within
// any range comment's [Line, EndLine] for the current file.
func (dv *diffView) isInCommentRange(rowIdx int) bool {
	if dv.comments == nil || dv.file == nil {
		return false
	}
	if rowIdx < 0 || rowIdx >= len(dv.rows) {
		return false
	}
	row := dv.rows[rowIdx]
	if row.kind != rowDiffPair || row.lineNum == 0 {
		return false
	}
	filename := dv.file.NewName
	if filename == "/dev/null" {
		filename = dv.file.OldName
	}
	for _, c := range dv.comments.Comments {
		if c.File == filename && c.EndLine > 0 && row.lineNum >= c.Line && row.lineNum <= c.EndLine {
			return true
		}
	}
	return false
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

// prefixBlankGutter prepends blank gutter space for rendered multi-line blocks.
// The gutter width is preserved, but no line number is shown.
func prefixBlankGutter(lines []string, prefix string) []string {
	if prefix == "" {
		return lines
	}
	out := make([]string, 0, len(lines))
	blank := strings.Repeat(" ", lipgloss.Width(prefix))
	for _, l := range lines {
		out = append(out, blank+l)
	}
	return out
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

func wrapToWidth(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{""}
	}
	if s == "" {
		return []string{""}
	}
	var out []string
	runes := []rune(s)
	start := 0
	for start < len(runes) {
		end := start
		width := 0
		for end < len(runes) {
			rw := lipgloss.Width(string(runes[end]))
			if rw <= 0 {
				rw = 1
			}
			if width+rw > maxWidth {
				break
			}
			width += rw
			end++
		}
		if end == start {
			end++
		}
		out = append(out, string(runes[start:end]))
		start = end
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func wrapMultilineToWidth(s string, maxWidth int) []string {
	src := strings.Split(s, "\n")
	if len(src) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(src))
	for _, line := range src {
		out = append(out, wrapToWidth(line, maxWidth)...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func splitRenderedLines(s string) []string {
	parts := strings.Split(s, "\n")
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

func blankWidth(w int) string {
	if w <= 0 {
		return ""
	}
	return strings.Repeat(" ", w)
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

// screenLinesForRow returns how many screen lines a single logical row
// occupies when rendered, accounting for soft-wrapping.
func (dv *diffView) screenLinesForRow(rowIdx int) int {
	if rowIdx < 0 || rowIdx >= len(dv.rows) {
		return 1
	}
	row := dv.rows[rowIdx]
	switch row.kind {
	case rowSpacer:
		return 1
	case rowHunkHeader:
		rg := 0
		if dv.showRelative() {
			rg = 4
		}
		contentW := dv.width - 4 - rg
		if contentW < 1 {
			contentW = 1
		}
		return len(dv.wrappedChunks(row.hunk, contentW))
	case rowComment:
		rg := 0
		if dv.showRelative() {
			rg = 4
		}
		commentBoxW := dv.width - 6 - rg
		if commentBoxW < 1 {
			commentBoxW = 1
		}
		commentInnerW := commentBoxW - 4
		if commentInnerW < 1 {
			commentInnerW = 1
		}
		text := formatCommentBubble(row.comment)
		wrapped := wrapMultilineToWidth(text, commentInnerW)
		return len(wrapped) + 2 // +2 for border top/bottom
	case rowDiffPair:
		if dv.mode == viewUnified {
			return dv.unifiedPairScreenLines(row)
		}
		return dv.sideBySidePairScreenLines(row)
	}
	return 1
}

func (dv *diffView) sideBySidePairScreenLines(row diffRow) int {
	rg := 0
	if dv.showRelative() {
		rg = 4
	}
	colWidth := (dv.width - 4 - (2 * rg)) / 2
	if colWidth < 20 {
		colWidth = 20
	}
	gutterWidth := 0
	if dv.showAbsolute() {
		gutterWidth = 6
	}
	textWidth := colWidth - gutterWidth
	if textWidth < 1 {
		textWidth = 1
	}
	pair := row.pair
	if pair == nil {
		return 1
	}
	leftN := 1 // nil side renders as 1 blank line
	rightN := 1
	if pair.Left != nil {
		leftN = len(dv.wrappedChunks(pair.Left.Content, textWidth))
	}
	if pair.Right != nil {
		rightN = len(dv.wrappedChunks(pair.Right.Content, textWidth))
	}
	if leftN > rightN {
		return leftN
	}
	return rightN
}

func (dv *diffView) unifiedPairScreenLines(row diffRow) int {
	rg := 0
	if dv.showRelative() {
		rg = 4
	}
	absGutter := 0
	if dv.showAbsolute() {
		absGutter = 12
	}
	contentWidth := dv.width - 4 - rg - absGutter
	textWidth := contentWidth - 1
	if textWidth < 1 {
		textWidth = 1
	}
	pair := row.pair
	if pair == nil {
		return 1
	}
	n := 0
	if pair.Left != nil && pair.Left.Op == diff.OpDelete {
		n += len(dv.wrappedChunks(pair.Left.Content, textWidth))
	}
	if pair.Right != nil && pair.Right.Op == diff.OpInsert {
		n += len(dv.wrappedChunks(pair.Right.Content, textWidth))
	}
	if pair.Left != nil && pair.Left.Op == diff.OpEqual {
		n += len(dv.wrappedChunks(pair.Left.Content, textWidth))
	}
	if n == 0 {
		n = 1
	}
	return n
}

// screenLinesInRange returns the total screen lines for rows [from, to).
func (dv *diffView) screenLinesInRange(from, to int) int {
	if from >= to {
		return 0
	}
	if from < 0 {
		from = 0
	}
	n := 0
	for i := from; i < to && i < len(dv.rows); i++ {
		n += dv.screenLinesForRow(i)
	}
	return n
}

// logicalRowsForScreenLines returns how many logical rows starting at startRow
// are needed to fill screenLines screen lines.
func (dv *diffView) logicalRowsForScreenLines(startRow, screenLines int) int {
	if screenLines <= 0 || startRow >= len(dv.rows) {
		return 0
	}
	if startRow < 0 {
		startRow = 0
	}
	accum := 0
	count := 0
	for i := startRow; i < len(dv.rows); i++ {
		accum += dv.screenLinesForRow(i)
		count++
		if accum >= screenLines {
			break
		}
	}
	return count
}

// lastVisibleRow returns the index of the last logical row whose top edge
// is within the viewport.
func (dv *diffView) lastVisibleRow() int {
	viewportHeight := dv.contentViewportHeight()
	if viewportHeight <= 0 || len(dv.rows) == 0 {
		return dv.scrollY
	}
	accum := 0
	last := dv.scrollY
	for i := dv.scrollY; i < len(dv.rows); i++ {
		if accum >= viewportHeight {
			break
		}
		last = i
		accum += dv.screenLinesForRow(i)
	}
	return last
}
