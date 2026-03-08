package output

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/review"
)

// FormatMarkdown formats a review as markdown suitable for agent consumption.
func FormatMarkdown(rev *review.Review, files []diff.FileDiff) string {
	var sb strings.Builder

	sb.WriteString("# Code Review\n\n")

	if rev.CommitHash != "" {
		sb.WriteString(fmt.Sprintf("**Commit:** %s %s\n\n", rev.CommitHash, rev.CommitSubject))
	}

	// Group comments by file
	commentsByFile := make(map[string][]review.Comment)
	for _, c := range rev.Comments {
		commentsByFile[c.File] = append(commentsByFile[c.File], c)
	}

	// Build a map of files for context
	fileMap := make(map[string]*diff.FileDiff)
	for i := range files {
		f := &files[i]
		name := f.NewName
		if name == "/dev/null" {
			name = f.OldName
		}
		fileMap[name] = f
	}

	// Sort filenames for deterministic output
	var filenames []string
	for filename := range commentsByFile {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	for _, filename := range filenames {
		comments := commentsByFile[filename]
		sort.SliceStable(comments, func(i, j int) bool {
			if comments[i].Line != comments[j].Line {
				return comments[i].Line < comments[j].Line
			}
			return comments[i].Text < comments[j].Text
		})

		sb.WriteString(fmt.Sprintf("## File: `%s`\n\n", filename))

		f := fileMap[filename]

		for _, comment := range comments {
			// Location header
			loc := formatLocation(filename, comment)
			sb.WriteString(fmt.Sprintf("### `%s`\n", loc))

			// Snippet in Read()-style cat -n format
			snippet := findSnippet(f, comment)
			if snippet != "" {
				ext := fileExtension(filename)
				sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n", ext, snippet))
			}

			// Feedback as blockquote
			lines := strings.Split(comment.Text, "\n")
			for _, line := range lines {
				sb.WriteString("> " + line + "\n")
			}
			sb.WriteString("\n")
		}
	}

	if gc := rev.GeneralComment(); gc != "" {
		sb.WriteString("## General Comments\n\n")
		sb.WriteString(gc + "\n")
	}

	if len(rev.Comments) == 0 && rev.GeneralComment() == "" {
		sb.WriteString("No comments.\n")
	}

	return sb.String()
}

// formatLocation returns "file:line" or "file:start-end" for range comments.
func formatLocation(filename string, c review.Comment) string {
	if c.EndLine > 0 {
		return fmt.Sprintf("%s:%d-%d", filename, c.Line, c.EndLine)
	}
	return fmt.Sprintf("%s:%d", filename, c.Line)
}

// findSnippet returns a Read()-style cat -n formatted snippet for a comment.
// For single-line comments: radius=2 (5 lines). For range comments: full range + 1 line padding.
func findSnippet(f *diff.FileDiff, c review.Comment) string {
	if f == nil {
		return ""
	}

	type entry struct {
		num     int
		content string
	}

	var entries []entry
	for _, h := range f.Hunks {
		for _, pair := range h.Pairs {
			e := entry{}
			if pair.Right != nil {
				e.num = pair.Right.NewNum
				e.content = pair.Right.Content
			} else if pair.Left != nil {
				e.num = pair.Left.OldNum
				e.content = pair.Left.Content
			}
			if e.num > 0 {
				entries = append(entries, e)
			}
		}
	}

	var startLine, endLine int
	if c.EndLine > 0 {
		// Range comment: full range + 1 line padding
		startLine = c.Line - 1
		endLine = c.EndLine + 1
	} else {
		// Single-line: radius of 2
		startLine = c.Line - 2
		endLine = c.Line + 2
	}

	// Find max line number for right-alignment
	maxNum := 0
	for _, e := range entries {
		if e.num >= startLine && e.num <= endLine && e.num > maxNum {
			maxNum = e.num
		}
	}
	if maxNum == 0 {
		return ""
	}

	// Determine width for right-aligned line numbers
	numWidth := len(fmt.Sprintf("%d", maxNum))
	if numWidth < 1 {
		numWidth = 1
	}

	var snippet []string
	for _, e := range entries {
		if e.num < startLine || e.num > endLine {
			continue
		}
		snippet = append(snippet, fmt.Sprintf("%*d\t%s", numWidth, e.num, e.content))
	}

	return strings.Join(snippet, "\n")
}

func fileExtension(filename string) string {
	parts := strings.Split(filename, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return ""
}
