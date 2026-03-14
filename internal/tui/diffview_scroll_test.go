package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/review"
)

func TestMoveCursorToScrollsNearBottomWithMargin(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.height = 20
	dv.setFile(makeLargeFileDiff(80), review.New())

	dv.moveCursorTo(15)

	if dv.scrollY != 2 {
		t.Fatalf("scrollY after moving near bottom = %d, want 2", dv.scrollY)
	}
}

func TestMoveCursorToScrollsNearTopWithMargin(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.height = 20
	dv.setFile(makeLargeFileDiff(80), review.New())

	dv.moveCursorTo(30)
	if dv.scrollY == 0 {
		t.Fatal("expected initial downward scroll to be non-zero")
	}

	dv.moveCursorTo(20)

	if dv.scrollY != 15 {
		t.Fatalf("scrollY after moving near top = %d, want 15", dv.scrollY)
	}
}

func TestSetFileKeepPositionPreservesInsertVsDeleteAnchor(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.height = 20
	dv.setFile(makeDuplicateLineNumDiff(), review.New())

	insertRow := -1
	for i, row := range dv.rows {
		if row.kind != rowDiffPair || row.lineNum != 2 || row.pair == nil {
			continue
		}
		if row.pair.Left == nil && row.pair.Right != nil {
			insertRow = i
			break
		}
	}
	if insertRow == -1 {
		t.Fatal("failed to find insert row for lineNum=2")
	}

	dv.cursorY = insertRow
	dv.scrollY = 0
	dv.setFileKeepPosition(makeDuplicateLineNumDiff(), review.New())

	row := dv.rows[dv.cursorY]
	if row.pair == nil || row.pair.Left != nil || row.pair.Right == nil {
		t.Fatalf("cursor anchor changed; got left=%v right=%v", row.pair.Left != nil, row.pair.Right != nil)
	}
}

func TestSetFileKeepPositionPreservesScreenRow(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.height = 20
	dv.setFile(makeLargeFileDiff(80), review.New())

	dv.cursorY = 30
	dv.scrollY = 15
	wantScreenRow := dv.cursorY - dv.scrollY
	wantLineNum := dv.rows[dv.cursorY].lineNum

	dv.setFileKeepPosition(makeLargeFileDiff(80), review.New())

	if got := dv.cursorY - dv.scrollY; got != wantScreenRow {
		t.Fatalf("screen row after keep-position = %d, want %d", got, wantScreenRow)
	}
	if got := dv.rows[dv.cursorY].lineNum; got != wantLineNum {
		t.Fatalf("line num after keep-position = %d, want %d", got, wantLineNum)
	}
}

func TestViewWithPathHeightStableForShortAndFullDiffs(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 12

	dv.setFile(makeLargeFileDiff(3), review.New())
	shortH := lipgloss.Height(dv.viewWithPath("path/to/file.go"))

	dv.setFile(makeLargeFileDiff(300), review.New())
	fullH := lipgloss.Height(dv.viewWithPath("path/to/file.go"))

	want := dv.height + 2 // content height + panel border
	if shortH != want {
		t.Fatalf("short diff panel height = %d, want %d", shortH, want)
	}
	if fullH != want {
		t.Fatalf("full diff panel height = %d, want %d", fullH, want)
	}
}

func TestViewSoftWrapsLongLineWithoutTruncation(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 60
	dv.height = 10
	dv.setFile(&diff.FileDiff{
		OldName: "wrap.go",
		NewName: "wrap.go",
		Hunks: []diff.Hunk{{
			OldStart: 1,
			OldCount: 1,
			NewStart: 1,
			NewCount: 1,
			Pairs: []diff.LinePair{{
				Left:  &diff.DiffLine{Op: diff.OpEqual, Content: "prefix " + strings.Repeat("x", 120) + " suffix", OldNum: 1, NewNum: 1},
				Right: &diff.DiffLine{Op: diff.OpEqual, Content: "prefix " + strings.Repeat("x", 120) + " suffix", OldNum: 1, NewNum: 1},
			}},
		}},
	}, review.New())

	out := dv.viewWithPath("wrap.go")
	if !strings.Contains(out, "suffix") {
		t.Fatalf("wrapped output missing tail content, got:\n%s", out)
	}
}

func TestWrappedSideBySideLineNumbersMatchBothSides(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 80

	line := &diff.DiffLine{
		Op:      diff.OpEqual,
		Content: strings.Repeat("abcdefghij", 12),
		OldNum:  42,
		NewNum:  42,
	}

	left := dv.renderSideWrapped(line, 30, true)
	right := dv.renderSideWrapped(line, 30, false)
	if len(left) < 2 || len(right) < 2 {
		t.Fatalf("expected wrapped output with multiple segments, got left=%d right=%d", len(left), len(right))
	}

	left0 := stripAnsi(left[0])
	right0 := stripAnsi(right[0])
	if !strings.Contains(left0, "42") || !strings.Contains(right0, "42") {
		t.Fatalf("first wrapped segment should show line numbers on both sides: left=%q right=%q", left0, right0)
	}

	left1 := stripAnsi(left[1])
	right1 := stripAnsi(right[1])
	if strings.Contains(left1, "0") || strings.Contains(right1, "0") {
		t.Fatalf("continuation segment should not render 0 line numbers: left=%q right=%q", left1, right1)
	}
}

func TestSideBySideShowsRelativeNumbersOnBothSides(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 8
	dv.setFile(makeLargeFileDiff(3), review.New())
	dv.cursorY = 1 // first diff row (after hunk header)
	dv.scrollY = 1

	out := dv.renderSideBySideContent()
	first := strings.Split(out, "\n")[0]
	plain := stripAnsi(first)
	if strings.Count(plain, "  0") < 2 {
		t.Fatalf("expected relative cursor number on both sides, got: %q", plain)
	}
}

func TestPrefixBlankGutter(t *testing.T) {
	t.Parallel()

	in := []string{"a", "b", "c"}
	got := prefixBlankGutter(in, "  0 ")
	want := []string{"    a", "    b", "    c"}
	if len(got) != len(want) {
		t.Fatalf("len(prefixBlankGutter)=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRelativeNumbersDoNotAdvanceAcrossCommentRows(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 8
	rev := review.New()
	rev.AddComment("x.go", 1, "note")
	dv.setFile(makeLargeFileDiff(2), rev)
	dv.cursorY = 1 // first diff row (line 1)

	if got := dv.relativeNumStr(2); got != "  0" {
		t.Fatalf("comment row relative = %q, want %q", got, "  0")
	}
	if got := dv.relativeNumStr(3); got != "  1" {
		t.Fatalf("next diff row relative = %q, want %q", got, "  1")
	}
}

func TestRenderCachesHighlightAcrossRedraws(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 10
	dv.setFile(makeLargeFileDiff(40), review.New())

	highlightCalls := 0
	dv.highlight = func(filename, content string) string {
		highlightCalls++
		return content
	}

	_ = dv.renderSideBySideContent()
	firstCalls := highlightCalls
	if firstCalls == 0 {
		t.Fatal("expected initial render to call highlighter")
	}

	_ = dv.renderSideBySideContent()
	if highlightCalls != firstCalls {
		t.Fatalf("expected cached redraw to avoid new highlight calls, got first=%d second=%d", firstCalls, highlightCalls)
	}

	dv.scrollY += 2
	_ = dv.renderSideBySideContent()
	if highlightCalls < firstCalls {
		t.Fatalf("unexpected highlight call count regression: first=%d now=%d", firstCalls, highlightCalls)
	}
}

func TestRenderCachesHighlightAcrossRedrawsUnified(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.mode = viewUnified
	dv.width = 100
	dv.height = 10
	dv.setFile(makeLargeFileDiff(40), review.New())

	highlightCalls := 0
	dv.highlight = func(filename, content string) string {
		highlightCalls++
		return content
	}

	_ = dv.renderUnifiedContent()
	firstCalls := highlightCalls
	if firstCalls == 0 {
		t.Fatal("expected initial unified render to call highlighter")
	}

	_ = dv.renderUnifiedContent()
	if highlightCalls != firstCalls {
		t.Fatalf("expected cached unified redraw to avoid new highlight calls, got first=%d second=%d", firstCalls, highlightCalls)
	}
}

func TestRenderCachesResetOnSetFile(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 10
	dv.setFile(makeLargeFileDiff(40), review.New())
	dv.highlight = func(filename, content string) string { return content }

	_ = dv.renderSideBySideContent()
	if len(dv.highlightCache) == 0 || len(dv.wrapCache) == 0 {
		t.Fatalf("expected caches to populate, got highlight=%d wrap=%d", len(dv.highlightCache), len(dv.wrapCache))
	}

	dv.setFile(makeLargeFileDiff(10), review.New())
	if len(dv.highlightCache) != 0 || len(dv.wrapCache) != 0 {
		t.Fatalf("expected caches reset on setFile, got highlight=%d wrap=%d", len(dv.highlightCache), len(dv.wrapCache))
	}
}

func TestRenderCachesResetOnSetFileKeepPosition(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 10
	base := makeLargeFileDiff(40)
	dv.setFile(base, review.New())
	dv.highlight = func(filename, content string) string { return content }

	_ = dv.renderSideBySideContent()
	if len(dv.highlightCache) == 0 || len(dv.wrapCache) == 0 {
		t.Fatalf("expected caches to populate, got highlight=%d wrap=%d", len(dv.highlightCache), len(dv.wrapCache))
	}

	dv.cursorY = 5
	dv.scrollY = 2
	oldWrapLen := len(dv.wrapCache)
	dv.setFileKeepPosition(makeLargeFileDiff(41), review.New())
	if len(dv.highlightCache) != 0 {
		t.Fatalf("expected highlight cache reset on setFileKeepPosition, got %d", len(dv.highlightCache))
	}
	// Wrap cache may be repopulated with new-file entries during scroll
	// position restoration (screenLinesForRow calls wrappedChunks), but
	// old entries must have been cleared first.
	if len(dv.wrapCache) >= oldWrapLen {
		t.Fatalf("expected wrap cache to have been reset (old=%d new=%d)", oldWrapLen, len(dv.wrapCache))
	}
}

func TestWrapCacheRespectsWidthChanges(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 10
	dv.setFile(makeLargeFileDiff(1), review.New())

	line := &diff.DiffLine{
		Op:      diff.OpEqual,
		Content: strings.Repeat("abcdefghij", 12),
		OldNum:  1,
		NewNum:  1,
	}

	wide := dv.renderSideWrapped(line, 50, true)
	narrow := dv.renderSideWrapped(line, 20, true)

	if len(narrow) <= len(wide) {
		t.Fatalf("expected narrower width to produce more wrapped lines, got wide=%d narrow=%d", len(wide), len(narrow))
	}
}

func TestWrapCacheRespectsWidthChangesUnified(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 10
	dv.setFile(makeLargeFileDiff(1), review.New())

	line := &diff.DiffLine{
		Op:      diff.OpEqual,
		Content: strings.Repeat("abcdefghij", 12),
		OldNum:  1,
		NewNum:  1,
	}

	wide := dv.renderUnifiedLineWrapped(line, 50)
	narrow := dv.renderUnifiedLineWrapped(line, 20)

	if len(narrow) <= len(wide) {
		t.Fatalf("expected narrower unified width to produce more wrapped lines, got wide=%d narrow=%d", len(wide), len(narrow))
	}
}

func TestHighlightChunkGuardsDoNotPopulateCache(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.highlight = nil

	if got := dv.highlightChunk("a.go", "x"); got != "x" {
		t.Fatalf("highlightChunk with nil highlighter = %q, want %q", got, "x")
	}
	if got := dv.highlightChunk("", "x"); got != "x" {
		t.Fatalf("highlightChunk with empty filename = %q, want %q", got, "x")
	}
	// install a highlighter but pass empty chunk
	dv.highlight = func(filename, content string) string { return "!" + content }
	if got := dv.highlightChunk("a.go", ""); got != "" {
		t.Fatalf("highlightChunk with empty chunk = %q, want empty", got)
	}
	if len(dv.highlightCache) != 0 {
		t.Fatalf("expected highlight cache to stay empty on guard paths, got %d entries", len(dv.highlightCache))
	}
}

func TestRenderCachesWithInsertDeleteLines(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 100
	dv.height = 10
	dv.setFile(makeDuplicateLineNumDiff(), review.New())

	highlightCalls := 0
	dv.highlight = func(filename, content string) string {
		highlightCalls++
		return content
	}

	_ = dv.renderSideBySideContent()
	firstCalls := highlightCalls
	if firstCalls == 0 {
		t.Fatal("expected highlight calls on first render")
	}
	_ = dv.renderSideBySideContent()
	if highlightCalls != firstCalls {
		t.Fatalf("expected cache hit on second render with insert/delete lines, first=%d second=%d", firstCalls, highlightCalls)
	}
}

func TestResetRenderCachesWhenAlreadyEmpty(t *testing.T) {
	t.Parallel()

	dv := newDiffView()
	dv.width = 80
	dv.height = 10

	dv.setFile(makeLargeFileDiff(3), review.New())
	if len(dv.highlightCache) != 0 || len(dv.wrapCache) != 0 {
		t.Fatalf("expected empty caches after initial setFile without render, got highlight=%d wrap=%d", len(dv.highlightCache), len(dv.wrapCache))
	}

	// Call setFile again without warming caches first.
	dv.setFile(makeLargeFileDiff(4), review.New())
	if len(dv.highlightCache) != 0 || len(dv.wrapCache) != 0 {
		t.Fatalf("expected empty caches after repeated setFile without render, got highlight=%d wrap=%d", len(dv.highlightCache), len(dv.wrapCache))
	}
}

func TestRenderSideBySideOutputStableAfterCacheWarmup(t *testing.T) {
	t.Parallel()

	file := makeLargeFileDiff(80)
	rev := review.New()

	dv := newDiffView()
	dv.width = 110
	dv.height = 14
	dv.setFile(file, rev)
	dv.cursorY = 12
	dv.scrollY = 6
	dv.highlight = func(filename, content string) string {
		return "[" + filename + "]" + content
	}

	first := dv.renderSideBySideContent()
	second := dv.renderSideBySideContent()
	if first != second {
		t.Fatal("side-by-side render changed after cache warmup")
	}
}

func TestRenderUnifiedOutputStableAfterCacheWarmup(t *testing.T) {
	t.Parallel()

	file := makeLargeFileDiff(80)
	rev := review.New()

	dv := newDiffView()
	dv.mode = viewUnified
	dv.width = 110
	dv.height = 14
	dv.setFile(file, rev)
	dv.cursorY = 12
	dv.scrollY = 6
	dv.highlight = func(filename, content string) string {
		return "[" + filename + "]" + content
	}

	first := dv.renderUnifiedContent()
	second := dv.renderUnifiedContent()
	if first != second {
		t.Fatal("unified render changed after cache warmup")
	}
}

func TestRenderOutputMatchesFreshInstanceWithSameState(t *testing.T) {
	t.Parallel()

	file := makeLargeFileDiff(90)
	rev := review.New()

	highlight := func(filename, content string) string {
		return "<hl>" + content
	}

	warm := newDiffView()
	warm.width = 120
	warm.height = 16
	warm.setFile(file, rev)
	warm.cursorY = 18
	warm.scrollY = 9
	warm.highlight = highlight
	_ = warm.renderSideBySideContent() // warm caches
	got := warm.renderSideBySideContent()

	fresh := newDiffView()
	fresh.width = 120
	fresh.height = 16
	fresh.setFile(file, rev)
	fresh.cursorY = 18
	fresh.scrollY = 9
	fresh.highlight = highlight
	want := fresh.renderSideBySideContent()

	if got != want {
		t.Fatal("cached render output differs from fresh render output for same state")
	}
}

func TestRenderUnifiedOutputMatchesFreshInstanceWithSameState(t *testing.T) {
	t.Parallel()

	file := makeLargeFileDiff(90)
	rev := review.New()

	highlight := func(filename, content string) string {
		return "<hl>" + content
	}

	warm := newDiffView()
	warm.mode = viewUnified
	warm.width = 120
	warm.height = 16
	warm.setFile(file, rev)
	warm.cursorY = 18
	warm.scrollY = 9
	warm.highlight = highlight
	_ = warm.renderUnifiedContent() // warm caches
	got := warm.renderUnifiedContent()

	fresh := newDiffView()
	fresh.mode = viewUnified
	fresh.width = 120
	fresh.height = 16
	fresh.setFile(file, rev)
	fresh.cursorY = 18
	fresh.scrollY = 9
	fresh.highlight = highlight
	want := fresh.renderUnifiedContent()

	if got != want {
		t.Fatal("cached unified render output differs from fresh render output for same state")
	}
}

func BenchmarkRenderSideBySideContent(b *testing.B) {
	dv := newDiffView()
	dv.width = 120
	dv.height = 28
	dv.setFile(makeLargeFileDiff(400), review.New())
	dv.highlight = func(filename, content string) string {
		return fmt.Sprintf("\x1b[32m%s\x1b[0m", content)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dv.renderSideBySideContent()
		dv.cursorY++
		if dv.cursorY >= len(dv.rows) {
			dv.cursorY = 0
		}
	}
}

func BenchmarkRenderUnifiedContent(b *testing.B) {
	dv := newDiffView()
	dv.mode = viewUnified
	dv.width = 120
	dv.height = 28
	dv.setFile(makeLargeFileDiff(400), review.New())
	dv.highlight = func(filename, content string) string {
		return fmt.Sprintf("\x1b[32m%s\x1b[0m", content)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dv.renderUnifiedContent()
		dv.cursorY++
		if dv.cursorY >= len(dv.rows) {
			dv.cursorY = 0
		}
	}
}

func makeLargeFileDiff(lines int) *diff.FileDiff {
	pairs := make([]diff.LinePair, 0, lines)
	for i := 1; i <= lines; i++ {
		pairs = append(pairs, diff.LinePair{
			Left:  &diff.DiffLine{Op: diff.OpEqual, Content: "line", OldNum: i, NewNum: i},
			Right: &diff.DiffLine{Op: diff.OpEqual, Content: "line", OldNum: i, NewNum: i},
		})
	}
	return &diff.FileDiff{
		OldName: "x.go",
		NewName: "x.go",
		Hunks: []diff.Hunk{{
			OldStart: 1,
			OldCount: lines,
			NewStart: 1,
			NewCount: lines,
			Pairs:    pairs,
		}},
	}
}

func makeDuplicateLineNumDiff() *diff.FileDiff {
	return &diff.FileDiff{
		OldName: "x.go",
		NewName: "x.go",
		Hunks: []diff.Hunk{{
			OldStart: 1,
			OldCount: 3,
			NewStart: 1,
			NewCount: 3,
			Pairs: []diff.LinePair{
				{
					Left:  &diff.DiffLine{Op: diff.OpEqual, Content: "a", OldNum: 1, NewNum: 1},
					Right: &diff.DiffLine{Op: diff.OpEqual, Content: "a", OldNum: 1, NewNum: 1},
				},
				{
					Left: &diff.DiffLine{Op: diff.OpDelete, Content: "old", OldNum: 2},
				},
				{
					Right: &diff.DiffLine{Op: diff.OpInsert, Content: "new", NewNum: 2},
				},
				{
					Left:  &diff.DiffLine{Op: diff.OpEqual, Content: "c", OldNum: 3, NewNum: 3},
					Right: &diff.DiffLine{Op: diff.OpEqual, Content: "c", OldNum: 3, NewNum: 3},
				},
			},
		}},
	}
}
