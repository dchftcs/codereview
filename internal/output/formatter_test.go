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
		GeneralComments:   []string{"overall notes\nsecond line"},
		CommitHash:        "abc123",
		CommitSubject:     "subject",
		DiffLeft:          "merge-base(main,HEAD)",
		DiffRight:         "HEAD",
		IncludesStaged:    true,
		IncludesUnstaged:  true,
		IncludesUntracked: true,
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

	// Files sorted alphabetically, general comments last
	assertOrdered(t, out, "## File: `a.go`", "## File: `missing.go`", "## File: `z.go`", "## General Comments")

	mustContain(t, out, "**Commit:** abc123 subject")
	mustContain(t, out, "## Diff Context")
	mustContain(t, out, "- LHS (base): `merge-base(main,HEAD)`")
	mustContain(t, out, "- RHS (target): `HEAD`")
	mustContain(t, out, "- Includes staged changes on RHS: yes")
	mustContain(t, out, "- Includes unstaged changes on RHS: yes")
	mustContain(t, out, "- Includes untracked files on RHS: yes")
	mustContain(t, out, "- Inline comment locations (`file:line` / `file:start-end`) are relative to this git diff view.")

	// New location format: file:line (no classification)
	mustContain(t, out, "### `a.go:1`")
	mustContain(t, out, "### `a.go:2`")
	mustContain(t, out, "### `a.go:9`")
	mustContain(t, out, "### `z.go:3`")
	mustContain(t, out, "### `missing.go:99`")

	// Read()-style cat -n snippets (right-aligned line numbers, tab-separated)
	mustContain(t, out, "1\tctx")
	mustContain(t, out, "2\tnew")
	mustContain(t, out, "3\tzadded")

	// Feedback as blockquotes
	mustContain(t, out, "**Comment:**")
	mustContain(t, out, "> modified line comment")
	mustContain(t, out, "> context line comment")
	mustContain(t, out, "> no file context")

	// General comments are a single free-form block (not bullets)
	mustContain(t, out, "overall notes")
	mustContain(t, out, "second line")
	mustNotContain(t, out, "- overall")

	// Old format should NOT be present
	mustNotContain(t, out, "(added)")
	mustNotContain(t, out, "(context)")
	mustNotContain(t, out, "(modified)")
	mustNotContain(t, out, "(deleted)")
	mustNotContain(t, out, "- Location:")
	mustNotContain(t, out, "- Feedback:")
	mustNotContain(t, out, "- Snippet:")
	mustNotContain(t, out, ">>")
}

func TestFormatMarkdownRangeComment(t *testing.T) {
	t.Parallel()

	rev := &review.Review{
		Comments: []review.Comment{
			{File: "main.go", Line: 10, EndLine: 25, Text: "this whole block needs refactoring"},
		},
	}

	files := []diff.FileDiff{
		{
			OldName: "main.go",
			NewName: "main.go",
			Hunks: []diff.Hunk{{
				Pairs: func() []diff.LinePair {
					var pairs []diff.LinePair
					for i := 8; i <= 27; i++ {
						pairs = append(pairs, diff.LinePair{
							Left:  &diff.DiffLine{Op: diff.OpEqual, Content: "line content", OldNum: i, NewNum: i},
							Right: &diff.DiffLine{Op: diff.OpEqual, Content: "line content", OldNum: i, NewNum: i},
						})
					}
					return pairs
				}(),
			}},
		},
	}

	out := FormatMarkdown(rev, files)

	// Range location format
	mustContain(t, out, "### `main.go:10-25`")

	// Snippet should include full range + 1 line padding
	mustContain(t, out, " 9\tline content") // 1 line before
	mustContain(t, out, "10\tline content")
	mustContain(t, out, "25\tline content")
	mustContain(t, out, "26\tline content") // 1 line after

	// Feedback as blockquote
	mustContain(t, out, "**Comment:**")
	mustContain(t, out, "> this whole block needs refactoring")
}

func TestFormatMarkdownReadStyleLineNumbers(t *testing.T) {
	t.Parallel()

	rev := &review.Review{
		Comments: []review.Comment{
			{File: "f.go", Line: 100, Text: "check"},
		},
	}

	files := []diff.FileDiff{
		{
			OldName: "f.go",
			NewName: "f.go",
			Hunks: []diff.Hunk{{
				Pairs: func() []diff.LinePair {
					var pairs []diff.LinePair
					for i := 98; i <= 102; i++ {
						pairs = append(pairs, diff.LinePair{
							Left:  &diff.DiffLine{Op: diff.OpEqual, Content: "code", OldNum: i, NewNum: i},
							Right: &diff.DiffLine{Op: diff.OpEqual, Content: "code", OldNum: i, NewNum: i},
						})
					}
					return pairs
				}(),
			}},
		},
	}

	out := FormatMarkdown(rev, files)

	// Line numbers should be right-aligned and tab-separated
	// 3-digit numbers: " 98\tcode" through "102\tcode"
	mustContain(t, out, " 98\tcode")
	mustContain(t, out, " 99\tcode")
	mustContain(t, out, "100\tcode")
	mustContain(t, out, "101\tcode")
	mustContain(t, out, "102\tcode")
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

	mustContain(t, out, "### `missing.go:42`")
	mustContain(t, out, "**Comment:**")
	mustContain(t, out, "> x")
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

func mustNotContain(t *testing.T, s, reject string) {
	t.Helper()
	if strings.Contains(s, reject) {
		t.Fatalf("output should not contain %q\n%s", reject, s)
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
