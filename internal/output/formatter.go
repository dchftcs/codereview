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
		sb.WriteString(fmt.Sprintf("Inline comments: %d\n\n", len(comments)))

		f := fileMap[filename]

		for i, comment := range comments {
			context := findLineSnippet(f, comment.Line)
			lineType := classifyLine(f, comment.Line)

			sb.WriteString(fmt.Sprintf("### Comment %d\n", i+1))
			sb.WriteString(fmt.Sprintf("- Location: line %d (%s)\n", comment.Line, lineType))
			if context != "" {
				ext := fileExtension(filename)
				sb.WriteString("- Snippet:\n")
				sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n", ext, context))
			} else {
				sb.WriteString("- Snippet: unavailable in current diff context\n")
			}
			sb.WriteString(fmt.Sprintf("- Feedback: %s\n\n", comment.Text))
		}
	}

	if len(rev.GeneralComments) > 0 {
		sb.WriteString("## General Comments\n\n")
		for _, gc := range rev.GeneralComments {
			sb.WriteString(fmt.Sprintf("- %s\n", gc))
		}
	}

	if len(rev.Comments) == 0 && len(rev.GeneralComments) == 0 {
		sb.WriteString("No comments.\n")
	}

	return sb.String()
}

func findLineSnippet(f *diff.FileDiff, lineNum int) string {
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
			if e.num > 0 && e.content != "" {
				entries = append(entries, e)
			}
		}
	}

	const radius = 2
	var snippet []string
	for _, e := range entries {
		if e.num < lineNum-radius || e.num > lineNum+radius {
			continue
		}
		prefix := "  "
		if e.num == lineNum {
			prefix = ">>"
		}
		snippet = append(snippet, fmt.Sprintf("%s %4d | %s", prefix, e.num, e.content))
	}

	return strings.Join(snippet, "\n")
}

func classifyLine(f *diff.FileDiff, lineNum int) string {
	if f == nil {
		return "context"
	}
	for _, h := range f.Hunks {
		for _, pair := range h.Pairs {
			if pair.Right != nil && pair.Right.NewNum == lineNum {
				switch pair.Right.Op {
				case diff.OpInsert:
					// If paired with a delete, it's a modification
					if pair.Left != nil && pair.Left.Op == diff.OpDelete {
						return "modified"
					}
					return "added"
				case diff.OpEqual:
					return "context"
				}
			}
			// Also check old-side only lines (deletions)
			if pair.Left != nil && pair.Right == nil && pair.Left.OldNum == lineNum {
				return "deleted"
			}
		}
	}
	return "context"
}

func fileExtension(filename string) string {
	parts := strings.Split(filename, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return ""
}
