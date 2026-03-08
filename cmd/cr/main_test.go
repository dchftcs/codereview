package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dc/codereview/internal/tui"
)

func TestParseArgsSuccessPaths(t *testing.T) {
	setEnv(t, "COLORFGBG", "")

	cases := []struct {
		name         string
		args         []string
		wantRev      string
		wantOutput   string
		wantBranch   bool
		wantUnstaged bool
		wantTheme    tui.ThemeName
	}{
		{name: "no args", args: []string{"cr"}, wantRev: "", wantOutput: "", wantBranch: false, wantUnstaged: false, wantTheme: tui.ThemeDark},
		{name: "positional rev", args: []string{"cr", "HEAD~1"}, wantRev: "HEAD~1", wantOutput: "", wantBranch: false, wantUnstaged: false, wantTheme: tui.ThemeDark},
		{name: "short output", args: []string{"cr", "-o", "review.md"}, wantRev: "", wantOutput: "review.md", wantBranch: false, wantUnstaged: false, wantTheme: tui.ThemeDark},
		{name: "long output", args: []string{"cr", "--output", "out.md"}, wantRev: "", wantOutput: "out.md", wantBranch: false, wantUnstaged: false, wantTheme: tui.ThemeDark},
		{name: "theme dark", args: []string{"cr", "--theme", "dark"}, wantRev: "", wantOutput: "", wantBranch: false, wantUnstaged: false, wantTheme: tui.ThemeDark},
		{name: "theme light equals", args: []string{"cr", "--theme=light"}, wantRev: "", wantOutput: "", wantBranch: false, wantUnstaged: false, wantTheme: tui.ThemeLight},
		{name: "branch", args: []string{"cr", "--branch"}, wantRev: "", wantOutput: "", wantBranch: true, wantUnstaged: false, wantTheme: tui.ThemeDark},
		{name: "unstaged", args: []string{"cr", "--unstaged"}, wantRev: "", wantOutput: "", wantBranch: false, wantUnstaged: true, wantTheme: tui.ThemeDark},
		{name: "unstaged short", args: []string{"cr", "-u"}, wantRev: "", wantOutput: "", wantBranch: false, wantUnstaged: true, wantTheme: tui.ThemeDark},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setArgs(t, tc.args...)
			rev, out, branch, unstaged, theme, err := parseArgs()
			if err != nil {
				t.Fatalf("parseArgs returned error: %v", err)
			}
			if rev != tc.wantRev || out != tc.wantOutput || branch != tc.wantBranch || unstaged != tc.wantUnstaged || theme != tc.wantTheme {
				t.Fatalf("parseArgs mismatch: got rev=%q out=%q branch=%v unstaged=%v theme=%q", rev, out, branch, unstaged, theme)
			}
		})
	}
}

func TestParseArgsErrors(t *testing.T) {
	setEnv(t, "COLORFGBG", "")

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing output short", args: []string{"cr", "-o"}, wantErr: "missing value for -o"},
		{name: "missing output long", args: []string{"cr", "--output"}, wantErr: "missing value for --output"},
		{name: "missing theme", args: []string{"cr", "--theme"}, wantErr: "missing value for --theme"},
		{name: "invalid theme", args: []string{"cr", "--theme", "solarized"}, wantErr: `invalid theme "solarized"`},
		{name: "branch and unstaged", args: []string{"cr", "--branch", "--unstaged"}, wantErr: "--unstaged cannot be combined with --branch"},
		{name: "rev and unstaged", args: []string{"cr", "HEAD~1", "--unstaged"}, wantErr: "--unstaged cannot be combined with a revision argument"},
		{name: "unknown flag", args: []string{"cr", "--wat"}, wantErr: "unknown flag: --wat"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setArgs(t, tc.args...)
			_, _, _, _, _, err := parseArgs()
			if err == nil {
				t.Fatal("parseArgs error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want contains %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseArgsDetectThemeFromEnv(t *testing.T) {
	setArgs(t, "cr")

	setEnv(t, "COLORFGBG", "15;9")
	_, _, _, _, theme, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if theme != tui.ThemeLight {
		t.Fatalf("theme = %q, want %q", theme, tui.ThemeLight)
	}

	setEnv(t, "COLORFGBG", "15;1")
	_, _, _, _, theme, err = parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if theme != tui.ThemeDark {
		t.Fatalf("theme = %q, want %q", theme, tui.ThemeDark)
	}
}

func setArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = old })
}

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if value == "" {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%s): %v", key, err)
		}
	} else {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Setenv(%s): %v", key, err)
		}
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
