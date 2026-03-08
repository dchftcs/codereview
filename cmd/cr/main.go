package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	gitpkg "github.com/dc/codereview/internal/git"
	"github.com/dc/codereview/internal/highlight"
	"github.com/dc/codereview/internal/output"
	"github.com/dc/codereview/internal/tui"
)

func main() {
	noArgs := len(os.Args) == 1
	revSpec, outputFile, branchMode, unstagedMode, theme, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printUsage()
		os.Exit(2)
	}

	// If --branch flag or no args on a feature branch, diff against default branch
	if !unstagedMode && (branchMode || revSpec == "") {
		branch, err := gitpkg.CurrentBranch()
		if err == nil {
			defaultBranch := gitpkg.DefaultBranch()
			if branchMode || (branch != defaultBranch && branch != "HEAD") {
				revSpec = defaultBranch + "...HEAD"
			}
		}
	}

	highlight.Init(string(theme))

	cfg := tui.Config{
		RevSpec:      revSpec,
		UnstagedOnly: unstagedMode,
		OutputFile:   outputFile,
		PromptSaveAs: noArgs,
		Highlight:    highlight.Line,
		Theme:        theme,
	}

	m := tui.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fm := finalModel.(tui.Model)
	if fm.IsSaving() {
		rev := fm.GetReview()
		files := fm.GetFilesForOutput()
		md := output.FormatMarkdown(rev, files)
		outPath := fm.GetOutputFile()
		if outPath != "" {
			if err := os.WriteFile(outPath, []byte(md), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Review saved to %s\n", outPath)
		} else {
			fmt.Print(md)
		}
	}
}

func parseArgs() (revSpec, outputFile string, branchMode, unstagedMode bool, theme tui.ThemeName, err error) {
	theme = detectTheme()
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Handle --theme=value
		if strings.HasPrefix(arg, "--theme=") {
			theme, err = parseThemeValue(strings.TrimPrefix(arg, "--theme="))
			if err != nil {
				return "", "", false, false, "", err
			}
			continue
		}

		switch arg {
		case "--theme":
			if i+1 >= len(args) {
				return "", "", false, false, "", fmt.Errorf("missing value for %s", arg)
			}
			theme, err = parseThemeValue(args[i+1])
			if err != nil {
				return "", "", false, false, "", err
			}
			i++
		case "--output", "-o":
			if i+1 >= len(args) {
				return "", "", false, false, "", fmt.Errorf("missing value for %s", arg)
			}
			outputFile = args[i+1]
			i++
		case "--branch", "-b":
			branchMode = true
		case "--unstaged", "-u":
			unstagedMode = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			if strings.HasPrefix(arg, "-") {
				return "", "", false, false, "", fmt.Errorf("unknown flag: %s", arg)
			}
			revSpec = arg
		}
	}
	if unstagedMode && branchMode {
		return "", "", false, false, "", fmt.Errorf("--unstaged cannot be combined with --branch")
	}
	if unstagedMode && revSpec != "" {
		return "", "", false, false, "", fmt.Errorf("--unstaged cannot be combined with a revision argument")
	}
	return
}

func parseThemeValue(val string) (tui.ThemeName, error) {
	switch strings.ToLower(val) {
	case "light":
		return tui.ThemeLight, nil
	case "dark":
		return tui.ThemeDark, nil
	default:
		return "", fmt.Errorf("invalid theme %q (expected dark|light)", val)
	}
}

// detectTheme guesses the terminal background from the COLORFGBG env var.
// Format is "fg;bg" where bg >= 8 typically indicates a light background.
func detectTheme() tui.ThemeName {
	val := os.Getenv("COLORFGBG")
	if val == "" {
		return tui.ThemeDark
	}
	parts := strings.Split(val, ";")
	if len(parts) < 2 {
		return tui.ThemeDark
	}
	bg, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return tui.ThemeDark
	}
	if bg >= 8 {
		return tui.ThemeLight
	}
	return tui.ThemeDark
}

func printUsage() {
	fmt.Println(`cr - Code Review TUI

Usage:
  cr                          Review current branch vs main/master + staged/unstaged/untracked (auto-detect)
  cr --branch, -b             Explicitly diff current branch against main/master
  cr --unstaged, -u           Review only unstaged tracked changes + untracked files
  cr HEAD~1                   Review last commit
  cr HEAD~3..HEAD             Review last 3 commits
  cr abc123                   Review specific commit
  cr abc123..def456           Review commit range
  cr --output review.md       Save review to file (default: stdout on save)
  cr --theme dark|light       Color theme (auto-detected from COLORFGBG)

Keys:
  j/k, ↑/↓       Scroll up/down through diff
  ]/[, →/←       Next/previous file
  h/l            Previous/next commit
  c              Enter comment mode at current line
  R              Edit general comment (single text block)
  Ctrl+r         Focus general comments panel
  Ctrl+s         Submit comment
  Enter          Newline in comment
  Ctrl+g         Open $EDITOR for comment
  Esc            Cancel comment / exit mode
  d              Delete comment at current line
  E              Edit comment at cursor
  e              Toggle full file context
  Tab            Toggle side-by-side vs unified diff
  Ctrl+d/u       Half page down/up
  s              Save & output review
  q              Quit`)
}
