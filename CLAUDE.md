# CLAUDE.md - Development Notes for `cr`

## Project

`cr` is a terminal-based code review tool for humans reviewing LLM-generated code. It presents git diffs in an interactive TUI with inline commenting, and outputs structured markdown that a coding agent can consume to address feedback.

## Build & Run

```bash
go build -o cr ./cmd/cr/    # build
go vet ./...                 # lint
./cr                         # run (reviews current branch vs main)
```

Requires Go 1.25+.

No test suite yet. Manual testing against real git repos.

## Architecture

```
cmd/cr/main.go              Entry point, arg parsing, output after quit
internal/
  git/git.go                Shells out to `git` CLI (diff, log, merge-base, branch detection)
  diff/model.go             Data types: FileDiff, Hunk, DiffLine, LinePair
  diff/parser.go            Parses unified diff via sourcegraph/go-diff, aligns side-by-side pairs
  highlight/highlight.go    Chroma syntax highlighting with lexer cache
  review/review.go          Comment storage (file, line, text), general comments, add/delete/query
  tui/app.go                Root Bubble Tea model, message routing, async diff loading
  tui/diffview.go           Side-by-side and unified diff rendering, cursor, scrolling, inline comment input
  tui/filelist.go           File list sidebar with change indicators and comment counts
  tui/bottombar.go          Reusable bottom-bar text input (used for general comments)
  tui/keys.go               Key bindings
  tui/styles.go             Lipgloss color/style definitions (Dracula-based palette)
  output/formatter.go       Markdown output with file grouping, line context, classification
```

## Key Design Decisions

- **Bubble Tea (Elm Architecture)**: Model/Update/View. All state lives in `Model`. Messages are the only way to trigger state changes.
- **Async git operations**: `loadDiff()` and `navigateCommit()` return `tea.Cmd` so git subprocesses don't block the UI.
- **Chroma for highlighting**: Pure Go, no external deps. Lexer is cached per file extension. Highlighting happens per-line at render time.
- **Row-based rendering**: Diff lines, hunk headers, and comments are flattened into a `[]diffRow` slice. Cursor and scroll operate on row indices. This simplifies scrolling, cursor positioning, and comment insertion.
- **Side-by-side alignment**: `alignPairs()` in `parser.go` pairs consecutive delete+insert sequences. Unpaired deletes/inserts get `nil` on the other side.
- **ANSI-aware truncation**: `truncate()` uses `lipgloss.Width()` to measure visible width. Falls back to stripped text when truncation is needed (loses highlighting on that line but avoids garbled escape sequences).
- **Per-file state preservation**: `fileStates map[int]fileState` on `Model` saves/restores scroll, cursor, and comment input state per file index. `expandedSet map[int]bool` tracks expand mode per file. State is saved before switching files (`]`/`[`) and restored on return. Both maps are reset on commit navigation.

## Conventions

- No external git library — shell out to `git` CLI via `internal/git/run()`.
- File paths in comments and output use the "new" side name (falls back to "old" for deletions).
- `/dev/null` signals added or deleted files in git diff output.
- Deterministic output: comments grouped by file, files sorted alphabetically.

## Common Pitfalls

- `diffView.highlight` must be set after `newDiffView()` — it's not part of the constructor.
- `navigateCommit` has a pointer receiver but is called from a value-receiver method (`updateNormal`). This works because Go copies the model, but be aware of the semantics.
- `buildRows()` must be called after any comment add/delete to refresh the flattened row list. `restoreState()` calls it internally.
- `fileStates` and `expandedSet` must be reset (re-initialized) when switching commits, since the file list changes entirely.
- `clampScroll()` is called from `updateLayout()` — if you add new places that change layout or row count, ensure it's called there too.
- Inline comment input lives in `diffView` (not the bottom bar). The bottom bar (`bottomBarInput`) is only for general comments.
- `GeneralComments` is a `[]string` (not a single string). The formatter renders all of them.
