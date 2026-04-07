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

Deploy to a remote SSH host or a running container:

```bash
scripts/deploy-remote.sh --target ssh --ssh-host app.example.com
scripts/deploy-remote.sh --target container --container my-dev-box --install-mode temporary
```

The deploy script copies the current repo state except `.git`, builds `./cmd/cr`
on the remote side, and installs the binary either into a temporary staging
directory or a persistent prefix such as `$HOME/.local/bin`.

## Usage

```bash
cr                              # Review current branch vs main/master (auto-detect)
cr internal/tui                 # Review only files under a relative path prefix
cr 'internal/tui/*.go'          # Review files matching a basic glob pattern
cr --remote app:/srv/repo       # Review a remote repo over SSH
cr --container api:/workspace   # Review a repo inside a Docker container
cr -b, --branch                 # Explicitly diff current branch against main/master
cr -u, --unstaged               # Review only unstaged tracked changes + untracked files
cr HEAD~1                       # Review the last commit
cr HEAD~3..HEAD                 # Review a range of commits
cr abc123                       # Review a specific commit
cr abc123..def456               # Review a commit range
cr -o review.md                 # Save review output to a file
```

When run with no arguments on a feature branch, `cr` automatically diffs against `main` (or `master`), showing branch changes from the merge base plus staged/unstaged/untracked working-tree changes.
In the file list, untracked files are marked with `??`.
`--unstaged` shows only unstaged tracked changes (working tree vs index) plus untracked files.
`--remote [user@]host:/path/to/repo` runs git and file reads over SSH against that remote working tree. Remote status polling is slower than local polling to keep repeated refreshes inexpensive.
`--container name:/path/to/repo` runs the same workflow via `docker exec -i <name> sh -lc ...` inside a running container.
If an argument could be interpreted as either a git revision and a path, `cr` prompts you to choose.
The SSH/container path flow currently starts from `git diff`, `git status`, and `git ls-files`, and reads individual file contents on demand when you open reference-file views. That keeps the initial remote load cheap while leaving room for finer-grained lazy loading later.

## Keys

| Key | Action |
|-----|--------|
| `j` / `k` (Vim) | Move cursor up/down |
| `↑` / `↓` | Move cursor up/down |
| `[count]j` / `[count]k` | Move by count (e.g. `9j`) |
| `H` / `L` | Move cursor to top / bottom visible line (screen-relative) |
| `gg` / `G` | Go to top / bottom |
| `[count]gg` / `[count]G` | Go to source line number |
| `PgDn` / `PgUp` | Page down/up by one visible viewport height (cursor-anchored) |
| `Ctrl+f` / `Ctrl+b` | Scroll window down/up by one visible viewport height |
| `/` | Search in diff |
| `n` / `N` | Next / previous search match |
| `]` / `[` | Next / previous file (includes read files; state preserved) |
| `←` / `→` | Previous / next unread file |
| `m` | Mark selected file read/unread |
| `a` | Toggle stage / unstage for the selected modified file |
| `Shift+click` (file list) | Open context menu to toggle read/unread |
| `h` / `l` | Previous / next commit |
| `c` | Add inline comment at cursor line |
| `R` | Edit general comment (review-level) |
| `Enter` | Insert newline in comment |
| `Ctrl+s` | Submit comment |
| `Ctrl+g` | Open `$EDITOR` at the current file/line, or open the comment buffer while editing a comment |
| `Esc` | Cancel comment |
| `d` | Delete comment at cursor line |
| `E` | Edit comment at cursor line |
| `Tab` | Toggle side-by-side / unified view |
| `e` | Toggle full file context (expand), preserving cursor anchor and screen row |
| `s` | Save review and exit (prompts for filename in default no-arg flow) |
| `?` | Show help screen |
| `q` / `Ctrl+c` | Quit (prompts if review has unsaved comments) |

In the file list, `^` marks a file that currently has staged changes in the index.

Use `H`/`L` when you want screen-relative jumps (top/bottom visible row) to quickly start scrolling from the current viewport edge. `PgDn`/`PgUp` move by exactly one visible viewport height with cursor anchoring. `Ctrl+f`/`Ctrl+b` perform viewport/window scrolling by one page. `gg`/`G` remain full-diff jumps (first/last row).
Long lines are soft-wrapped in the diff view (not truncated).

## Navigation Guarantees

- File navigation (`]` / `[`) restores per-file cursor/scroll/comment-input state.
- Arrow navigation (`←` / `→`) skips files marked as read.
- Expand/collapse (`e`) keeps the cursor anchored to the same logical diff row identity.
- Expand/collapse (`e`) keeps the cursor on the same on-screen row when feasible.

## Output

Pressing `s` outputs a markdown document (to stdout or a file with `-o`) structured for a coding agent:

````markdown
# Code Review

**Commit:** abc1234 feat: add user auth

## Diff Context

- LHS (base): `merge-base(main,HEAD)`
- RHS (target): `HEAD`
- Includes staged changes on RHS: yes
- Includes unstaged changes on RHS: yes
- Includes untracked files on RHS: yes
- Inline comment locations (`file:line` / `file:start-end`) are relative to this git diff view.

## File: `src/auth.go`

### `src/auth.go:42`
```go
40	func isAllowed(user User) bool {
41		// ...
42	if user.IsAdmin() {
43		return true
44	}
```
**Comment:**
> This check is missing rate limiting.

### `src/auth.go:78`
```go
76	func validateToken(t string) bool {
77		// TODO: signature + expiry validation
78		return len(t) > 0
79	}
```
**Comment:**
> Token validation should check signature and expiry.

## General Comments

Add tests for invalid/expired tokens.
````

Inline comments are grouped by file, files are sorted alphabetically, and comments are sorted by line number.  
`REVIEW.md` includes a **Diff Context** section that defines LHS/RHS and whether RHS includes staged/unstaged/untracked changes.  
Inline comment locations are diff-view-relative (`file:line` / `file:start-end` against that LHS/RHS context), followed by an optional snippet and a labeled `Comment` block.  
If no snippet can be resolved from the current diff context, only the location and `Comment` block are emitted.

## Requirements

- Go 1.25+
- `git` CLI available on PATH
- A terminal with 256-color support (most modern terminals)
