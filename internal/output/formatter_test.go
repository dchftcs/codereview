package output

import (
	"strings"
	"testing"

	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/review"
)

func TestFormatMarkdownDeterministicFileOrderingAndInlineBlocks(t *testing.T) {
	t.Parallel()

	rev := &review.Review{
		Comments: []review.Comment{
			{File: "z.go", Line: 3, Text: "added line comment"},
			{File: "a.go", Line: 2, Text: "modified line comment"},
			{File: "a.go", Line: 9, Text: "deleted line comment"},
			{File: "a.go", Line: 1, Text: "context line comment"},
			{File: "missing.go", Line: 99, Text: "no file context"},
		},
		GeneralComments: []string{"overall 1", "overall 2"},
		CommitHash:      "abc123",
		CommitSubject:   "subject",
	}

	files := []diff.FileDiff{
		{
			OldName: "a.go",
			NewName: "a.go",
			Hunks: []diff.Hunk{{
				Pairs: []diff.LinePair{
					{Left: &diff.DiffLine{Op: diff.OpEqual, Content: "ctx", OldNum: 1, NewNum: 1}, Right: &diff.DiffLine{Op: diff.OpEqual, Content: "ctx", OldNum: 1, NewNum: 1}},
					{Left: &diff.DiffLine{Op: diff.OpDelete, Content: "old", OldNum: 2}, Right: &diff.DiffLine{Op: diff.OpInsert, Content: "new", NewNum: 2}},
					{Right: &diff.DiffLine{Op: diff.OpInsert, Content: "added", NewNum: 3}},
					{Left: &diff.DiffLine{Op: diff.OpDelete, Content: "removed", OldNum: 9}},
				},
			}},
		},
		{
			OldName: "z.go",
			NewName: "z.go",
			Hunks: []diff.Hunk{{
				Pairs: []diff.LinePair{{Right: &diff.DiffLine{Op: diff.OpInsert, Content: "zadded", NewNum: 3}}},
			}},
		},
	}

	out := FormatMarkdown(rev, files)

	assertOrdered(t, out, "## File: `a.go`", "## File: `missing.go`", "## File: `z.go`", "## General Comments")

	mustContain(t, out, "**Commit:** abc123 subject")
	mustContain(t, out, "Inline comments: 3")
	mustContain(t, out, "Inline comments: 1")
	mustContain(t, out, "- Location: line 2 (modified)")
	mustContain(t, out, "- Location: line 3 (added)")
	mustContain(t, out, "- Location: line 1 (context)")
	mustContain(t, out, "- Location: line 9 (deleted)")
	mustContain(t, out, "- Feedback: no file context")
	mustContain(t, out, ">>    2 | new")
	mustContain(t, out, "   1 | ctx")
	mustContain(t, out, "- overall 1")
	mustContain(t, out, "- overall 2")
}

func TestFormatMarkdownNoComments(t *testing.T) {
	t.Parallel()

	out := FormatMarkdown(&review.Review{}, nil)
	mustContain(t, out, "# Code Review")
	mustContain(t, out, "No comments.")
}

func TestFormatMarkdownMissingFileContextNoCodeBlock(t *testing.T) {
	t.Parallel()

	rev := &review.Review{Comments: []review.Comment{{File: "missing.go", Line: 42, Text: "x"}}}
	out := FormatMarkdown(rev, nil)

	mustContain(t, out, "- Location: line 42 (context)")
	mustContain(t, out, "- Snippet: unavailable in current diff context")
	if strings.Contains(out, "```") {
		t.Fatalf("expected no code fence when file context is missing, got:\n%s", out)
	}
}

func mustContain(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("output missing %q\n%s", want, s)
	}
}

func assertOrdered(t *testing.T, s string, parts ...string) {
	t.Helper()
	pos := -1
	for _, p := range parts {
		i := strings.Index(s, p)
		if i == -1 {
			t.Fatalf("output missing %q\n%s", p, s)
		}
		if i < pos {
			t.Fatalf("output order mismatch for %q\n%s", p, s)
		}
		pos = i
	}
}
