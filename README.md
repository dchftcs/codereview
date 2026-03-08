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

When run with no arguments on a feature branch, `cr` automatically diffs against `main` (or `master`), showing branch changes from the merge base plus staged/unstaged/untracked working-tree changes.
In the file list, untracked files are marked with `??`.

## Keys

| Key | Action |
|-----|--------|
| `j` / `k` (Vim) | Move cursor up/down |
| `↑` / `↓` | Move cursor up/down |
| `[count]j` / `[count]k` | Move by count (e.g. `9j`) |
| `H` / `L` | Move cursor to top / bottom visible line (screen-relative) |
| `gg` / `G` | Go to top / bottom |
| `[count]gg` / `[count]G` | Go to line number |
| `PgDn` / `PgUp` / `Ctrl+f` / `Ctrl+b` | Page down/up by one visible viewport height |
| `Ctrl+d` / `Ctrl+u` | Half page down/up |
| `/` | Search in diff |
| `n` / `N` | Next / previous search match |
| `]` / `[` | Next / previous file (state preserved) |
| `h` / `l` / `←` / `→` | Previous / next commit |
| `c` | Add inline comment at cursor line |
| `R` | Edit general comment (review-level) |
| `Enter` | Insert newline in comment |
| `Ctrl+s` | Submit comment |
| `Ctrl+g` | Open `$EDITOR` for comment |
| `Esc` | Cancel comment |
| `d` | Delete comment at cursor line |
| `E` | Edit comment at cursor line |
| `Tab` | Toggle side-by-side / unified view |
| `e` | Toggle full file context (expand), preserving cursor anchor and screen row |
| `s` | Save review and exit (prompts for filename in default no-arg flow) |
| `?` | Show help screen |
| `q` / `Ctrl+c` | Quit (prompts if review has unsaved comments) |

Use `H`/`L` when you want screen-relative jumps (top/bottom visible row) to quickly start scrolling from the current viewport edge. `PgDn`/`PgUp` move by exactly one visible viewport height. `gg`/`G` remain full-diff jumps (first/last row).
Long lines are soft-wrapped in the diff view (not truncated).

## Navigation Guarantees

- File navigation (`]` / `[`) restores per-file cursor/scroll/comment-input state.
- Expand/collapse (`e`) keeps the cursor anchored to the same logical diff row identity.
- Expand/collapse (`e`) keeps the cursor on the same on-screen row when feasible.

## Output

Pressing `s` outputs a markdown document (to stdout or a file with `-o`) structured for a coding agent:

````markdown
# Code Review

**Commit:** abc1234 feat: add user auth

## File: `src/auth.go`

Inline comments: 2

### Comment 1
- Location: line 42 (added)
- Snippet:
```go
     40 | func isAllowed(user User) bool {
     41 |     // ...
>>   42 | if user.IsAdmin() {
     43 |     return true
     44 | }
```
- Feedback: This check is missing rate limiting.

### Comment 2
- Location: line 78 (modified)
- Snippet:
```go
     76 | func validateToken(t string) bool {
     77 |     // TODO: signature + expiry validation
>>   78 |     return len(t) > 0
     79 | }
```
- Feedback: Token validation should check signature and expiry.

## General Comments

Add tests for invalid/expired tokens.
````

Inline comments are grouped by file, files are sorted alphabetically, and comments are sorted by line number.  
Each inline comment includes the location (`added` / `modified` / `context` / `deleted`), a small snippet centered on the commented line, and feedback text.  
If no snippet can be resolved from the current diff context, output uses:

```markdown
- Snippet: unavailable in current diff context
```

## Requirements

- Go 1.25+
- `git` CLI available on PATH
- A terminal with 256-color support (most modern terminals)
