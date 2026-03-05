package output

import (
	"fmt"
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

	for filename, comments := range commentsByFile {
		sb.WriteString(fmt.Sprintf("## %s\n\n", filename))

		f := fileMap[filename]

		for _, comment := range comments {
			// Find the line context
			context := findLineContext(f, comment.Line)
			lineType := classifyLine(f, comment.Line)

			sb.WriteString(fmt.Sprintf("### Line %d (%s)\n", comment.Line, lineType))
			if context != "" {
				ext := fileExtension(filename)
				sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n", ext, context))
			}
			sb.WriteString(fmt.Sprintf("**Comment:** %s\n\n", comment.Text))
		}
	}

	if rev.GeneralComment != "" {
		sb.WriteString("## General Comments\n\n")
		sb.WriteString(fmt.Sprintf("- %s\n", rev.GeneralComment))
	}

	if len(rev.Comments) == 0 && rev.GeneralComment == "" {
		sb.WriteString("No comments.\n")
	}

	return sb.String()
}

func findLineContext(f *diff.FileDiff, lineNum int) string {
	if f == nil {
		return ""
	}
	// Collect up to 3 lines of context around the target line
	var contextLines []string
	for _, h := range f.Hunks {
		for _, pair := range h.Pairs {
			num := 0
			var content string
			if pair.Right != nil {
				num = pair.Right.NewNum
				content = pair.Right.Content
			} else if pair.Left != nil {
				num = pair.Left.OldNum
				content = pair.Left.Content
			}
			if num >= lineNum-1 && num <= lineNum+1 && content != "" {
				contextLines = append(contextLines, content)
			}
		}
	}
	return strings.Join(contextLines, "\n")
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
					return "added"
				case diff.OpEqual:
					if pair.Left != nil && pair.Left.Op == diff.OpDelete {
						return "modified"
					}
					return "context"
				}
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
