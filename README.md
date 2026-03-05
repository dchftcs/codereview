# cr - Code Review TUI

A terminal-based code review tool for reviewing LLM-generated code changes. Navigate diffs, add inline comments, and export structured feedback that a coding agent can act on.

## Install

```bash
go install ./cmd/cr/
```

Or build locally:

```bash
go build -o cr ./cmd/cr/
```

## Usage

```bash
cr                              # Review current branch vs main/master (auto-detect)
cr -b, --branch                 # Explicitly diff current branch against main/master
cr HEAD~1                       # Review the last commit
cr HEAD~3..HEAD                 # Review a range of commits
cr abc123                       # Review a specific commit
cr abc123..def456               # Review a commit range
cr -o review.md                 # Save review output to a file
```

When run with no arguments on a feature branch, `cr` automatically diffs against `main` (or `master`), showing only the branch's changes from the merge base.

## Keys

| Key | Action |
|-----|--------|
| `j` / `k` (Vim) | Move cursor up/down |
| `↑` / `↓` | Move cursor up/down |
| `[count]j` / `[count]k` | Move by count (e.g. `9j`) |
| `gg` / `G` | Go to top / bottom |
| `[count]gg` / `[count]G` | Go to line number |
| `Ctrl+d` / `Ctrl+u` | Half page down/up |
| `/` | Search in diff |
| `n` / `N` | Next / previous search match |
| `]` / `[` | Next / previous file (state preserved) |
| `h` / `l` / `←` / `→` | Previous / next commit |
| `c` | Add inline comment at cursor line |
| `R` | Add general comment (review-level) |
| `Enter` | Submit comment |
| `Esc` | Cancel comment |
| `d` | Delete comment at cursor line |
| `E` | Edit comment at cursor line |
| `Tab` | Toggle side-by-side / unified view |
| `e` | Toggle full file context (expand) |
| `s` | Save review and exit (prompts for filename in default no-arg flow) |
| `?` | Show help screen |
| `q` / `Ctrl+c` | Quit (prompts if review has unsaved comments) |

## Output

Pressing `s` outputs a markdown document (to stdout or a file with `-o`) structured for a coding agent:

```markdown
# Code Review

**Commit:** abc1234 feat: add user auth

## src/auth.go

### Line 42 (added)
\```go
if user.IsAdmin() {
\```
**Comment:** This check is missing rate limiting.

### Line 78 (modified)
\```go
func validateToken(t string) bool {
    return len(t) > 0
}
\```
**Comment:** Token validation should check signature and expiry.
```

Each comment includes the file path, line number, classification (added/modified/context/deleted), surrounding code context, and the review comment. This format is designed to be directly consumable by LLM coding agents.

## Requirements

- Go 1.25+
- `git` CLI available on PATH
- A terminal with 256-color support (most modern terminals)
