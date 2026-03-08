package tui

import (
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
