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

Tests:

```bash
go test ./...
```

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
  tui/contextmenu.go        Shift+click context menu: rendering, overlay, action dispatch
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
- **Dual jump semantics**: `gg`/`G` (plus `[count]gg`/`[count]G`) are absolute-row jumps for Vim parity. `H`/`L` are viewport-relative jumps (top/bottom visible line) for scroll priming from the current screen. `PgDn`/`PgUp` page by one visible viewport height.
- **Side-by-side alignment**: `alignPairs()` in `parser.go` pairs consecutive delete+insert sequences. Unpaired deletes/inserts get `nil` on the other side.
- **ANSI-aware truncation**: `truncate()` uses `lipgloss.Width()` to measure visible width. Falls back to stripped text when truncation is needed (loses highlighting on that line but avoids garbled escape sequences).
- **Mouse support**: Enabled via `tea.WithMouseCellMotion()`. Left-click in the file list selects a file (or toggles a directory); left-click in the diff view moves the cursor. Click-and-drag in the diff panel creates a visual selection: press sets the anchor, motion enters `modeVisual` and extends the selection, release finalizes it. A plain click (no drag) cancels any active visual selection. `handleMouseClick()` sets `mouseDrag` state on diff panel clicks; `handleMouseDrag()` enters visual mode when the mouse moves to a different row. `handleMouseClick()` in `app.go` maps screen coordinates to panel-relative positions using header height and panel borders.
- **Per-file state preservation**: `fileStates map[int]fileState` on `Model` saves/restores scroll, cursor, and comment input state per file index. `expandedSet map[int]bool` tracks expand mode per file. State is saved before switching files (`]`/`[`/`←`/`→`) and restored on return. Both maps are reset on commit navigation.
- **Expand/collapse stability invariant**: toggling `e` must preserve both (1) the anchored diff row identity (not just a raw line number) and (2) the cursor's on-screen row when feasible. Keep-position transitions should not apply normal scroll-margin nudging.
- **Multi-line comments via textarea**: Both inline and general comment input use `textarea.Model` (not `textinput.Model`). Enter inserts newlines; `ctrl+s` submits; `ctrl+g` opens `$EDITOR` (falls back to `$VISUAL`, then `vi`) via `tea.ExecProcess`. The bottom bar still uses `textinput.Model` for single-line prompts (search, save-as, etc.).
- **General comments viewer (`ctrl+r`)**: `modeViewGeneral` renders a centered overlay listing all general comments. `j/k` navigate, `d` deletes, `E` edits (enters `modeGeneralComment` with textarea pre-populated), `R` adds new. General comment input also renders as a centered overlay (not the bottom bar). `generalEditIdx` on `Model` tracks whether we're editing (`>=0`) or creating (`-1`).
- **Editor integration**: `ctrl+g` writes current text to a temp file, spawns the editor, reads back on exit, and cleans up. Both inline comments (`editorFinishedMsg`) and general comments (`generalEditorFinishedMsg`) have their own message types.
- **Context menu (`Shift+click`)**: `modeContextMenu` renders a small menu box at the click position via character-level overlay (`overlayAt()`). Uses Shift+left-click rather than right-click because most terminal emulators intercept right-click for their own paste menu. The `MouseMsg.Shift` bool distinguishes a plain click (cursor/selection) from a menu-opening click. Menu items vary by panel: diff panel offers "Add comment" and "Copy line" (`atotto/clipboard`); file list offers "Toggle expand". `j`/`k`/`Enter` navigate and select; any other key or click-outside dismisses. All state lives in `ctxMenu contextMenu` on `Model`. Styles use purple rounded border with blue highlight for the selected item.

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
- For expand/collapse transitions, use the keep-position path and keep-position clamping (not the normal margin-enforcing scroll path), or you'll regress stable-location behavior.
- Inline comment input lives in `diffView` (not the bottom bar). General comment input uses a dedicated `generalInput textarea.Model` rendered as a centered overlay. The bottom bar (`bottomBarInput`) is only for single-line prompts (search, save-as, etc.).
- `GeneralComments` is a `[]string` (not a single string). The formatter renders all of them. The footer shows their count (e.g. `"3 comments, 1 general"`), and a transient `statusMsg` confirms creation.
- Arrow keys `←`/`→` are bound to file navigation (`PrevFile`/`NextFile`), not commit navigation. Commit navigation uses only `h`/`l`.
- The inline comment `commentInput` is a `textarea.Model`, not a `textinput.Model`. Submit is `ctrl+s`, bound as `keys.SubmitComment`, not `Enter` (`keys.Submit`).
- General comment input (`modeGeneralComment`) uses `generalInput textarea.Model` on `Model`, not the bottom bar. The bottom bar is no longer used for general comments.
- The context menu uses Shift+left-click, not right-click. Right-click is intercepted by most terminal emulators (paste menu), so `MouseMsg` with `Button == MouseButtonRight` is unreliable. `MouseMsg.Shift` on a left-click is reliably passed through via SGR mouse encoding.
- The context menu overlay (`overlayAt()`) works on stripped (non-ANSI) text. It replaces characters in the base view at the menu's screen coordinates. This means ANSI styling is lost on overlaid lines — acceptable for a small menu box but would not scale to large overlays.
