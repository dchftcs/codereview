package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dc/codereview/internal/diff"
	gitpkg "github.com/dc/codereview/internal/git"
	"github.com/dc/codereview/internal/highlight"
	"github.com/dc/codereview/internal/output"
	"github.com/dc/codereview/internal/tui"
)

func main() {
	revSpec, outputFile := parseArgs()

	cfg := tui.Config{
		RevSpec:    revSpec,
		OutputFile: outputFile,
		Highlight:  highlight.Line,
	}

	m := tui.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fm := finalModel.(tui.Model)
	if fm.IsSaving() {
		rev := fm.GetReview()
		// Re-fetch diff for the output formatter
		rawDiff, err := gitpkg.Diff(revSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting diff: %v\n", err)
			os.Exit(1)
		}
		files, err := diff.Parse(rawDiff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing diff: %v\n", err)
			os.Exit(1)
		}

		md := output.FormatMarkdown(rev, files)

		if outputFile != "" {
			if err := os.WriteFile(outputFile, []byte(md), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Review saved to %s\n", outputFile)
		} else {
			fmt.Print(md)
		}
	}
}

func parseArgs() (revSpec, outputFile string) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--output", "-o":
			if i+1 < len(args) {
				outputFile = args[i+1]
				i++
			}
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			if !strings.HasPrefix(args[i], "-") {
				revSpec = args[i]
			}
		}
	}
	return
}

func printUsage() {
	fmt.Println(`cr - Code Review TUI

Usage:
  cr                          Review uncommitted changes (git diff)
  cr HEAD~1                   Review last commit
  cr HEAD~3..HEAD             Review last 3 commits
  cr abc123                   Review specific commit
  cr abc123..def456           Review commit range
  cr --output review.md       Save review to file (default: stdout on save)

Keys:
  j/k, arrows    Scroll up/down through diff
  n/N            Next/previous file
  h/l, ←/→       Previous/next commit
  c              Enter comment mode at current line
  Enter          Submit comment
  Esc            Cancel comment / exit mode
  d              Delete comment at current line
  e              Toggle expanded view (hide file list)
  Tab            Toggle side-by-side vs unified diff
  Ctrl+d/u       Half page down/up
  s              Save & output review
  q              Quit`)
}
