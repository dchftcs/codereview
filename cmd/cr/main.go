package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	targetArg, outputFile, branchMode, unstagedMode, theme, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printUsage()
		os.Exit(2)
	}
	resolved, err := resolveMainArgument(targetArg, gitpkg.IsRevision, pathExists, os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printUsage()
		os.Exit(2)
	}
	revSpec := resolved.RevSpec
	pathFilter := resolved.PathFilter

	// If the path filter points to a directory outside the current repo
	// (e.g. "../other-repo/"), chdir there and review that repo instead.
	if pathFilter != "" {
		if abs, err := filepath.Abs(targetArg); err == nil {
			if info, err := os.Stat(abs); err == nil && info.IsDir() {
				if isOutsideRepo(abs) {
					if err := os.Chdir(abs); err != nil {
						fmt.Fprintf(os.Stderr, "Error: cannot chdir to %s: %v\n", abs, err)
						os.Exit(1)
					}
					pathFilter = ""
				}
			}
		}
	}

	if unstagedMode && revSpec != "" {
		fmt.Fprintf(os.Stderr, "Error: --unstaged cannot be combined with a revision argument\n\n")
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
		PathFilter:   pathFilter,
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
			// Treat current-directory pathspec aliases like no positional revision.
			if arg == "." || arg == "./" {
				continue
			}
			revSpec = arg
		}
	}
	if unstagedMode && branchMode {
		return "", "", false, false, "", fmt.Errorf("--unstaged cannot be combined with --branch")
	}
	return
}

type resolvedArg struct {
	RevSpec    string
	PathFilter string
}

func resolveMainArgument(arg string, isRevision func(string) bool, pathExists func(string) bool, in io.Reader, out io.Writer) (resolvedArg, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" || arg == "." || arg == "./" {
		return resolvedArg{}, nil
	}

	normalizedPath := normalizePathArg(arg)
	pathLike := looksLikePathArg(arg) || pathExists(arg)
	if hasGlobPattern(arg) {
		pathLike = true
	}
	revOK := false
	if !hasGlobPattern(arg) {
		revOK = isRevision(arg)
	}

	if revOK && pathLike {
		asPath, err := promptArgumentDisambiguation(arg, in, out)
		if err != nil {
			return resolvedArg{}, err
		}
		if asPath {
			return resolvedArg{PathFilter: normalizedPath}, nil
		}
		return resolvedArg{RevSpec: arg}, nil
	}
	if pathLike {
		return resolvedArg{PathFilter: normalizedPath}, nil
	}
	if revOK {
		return resolvedArg{RevSpec: arg}, nil
	}
	return resolvedArg{RevSpec: arg}, nil
}

func promptArgumentDisambiguation(arg string, in io.Reader, out io.Writer) (bool, error) {
	r := bufio.NewReader(in)
	for {
		if _, err := fmt.Fprintf(out, "Argument %q matches both a git revision and a path. Use as path filter? [y/N]: ", arg); err != nil {
			return false, err
		}
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}
		choice := strings.ToLower(strings.TrimSpace(line))
		if choice == "y" || choice == "yes" {
			return true, nil
		}
		if choice == "" || choice == "n" || choice == "no" {
			return false, nil
		}
		if err == io.EOF {
			return false, nil
		}
	}
}

// isOutsideRepo reports whether abs is outside the current git repository's
// working tree (or the repo root cannot be determined).
func isOutsideRepo(abs string) bool {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	root := strings.TrimSpace(string(out))
	return !strings.HasPrefix(abs, root+string(filepath.Separator)) && abs != root
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func looksLikePathArg(arg string) bool {
	if arg == "" {
		return false
	}
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") || strings.HasPrefix(arg, "/") {
		return true
	}
	if strings.Contains(arg, "/") || strings.Contains(arg, `\`) {
		return true
	}
	return hasGlobPattern(arg)
}

func hasGlobPattern(arg string) bool {
	return strings.ContainsAny(arg, "*?[")
}

func normalizePathArg(arg string) string {
	p := filepath.ToSlash(strings.TrimSpace(arg))
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	return p
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
  cr internal/tui             Review only files under this relative path prefix
  cr 'internal/tui/*.go'      Review files matching glob (basic glob support)
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
  ]/[            Next/previous file (includes read)
  →/←            Next/previous unread file
  m              Mark selected file read/unread
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
  s              Save & output review
  q              Quit`)
}
