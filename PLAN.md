# Refactoring Plan

## Goals

1. Make comment input inline (render at cursor position within the diff, not at the bottom)
2. Add general comment support (review-level and file-level comments via `G` key)
3. Show comment indicators in the file list sidebar
4. Call `clampScroll()` after layout changes to prevent out-of-bounds cursor/scroll

---

## Refactor 1: Inline Comment Input

**Problem:** When the user presses `c`, a text input appears at the bottom of the screen between the diff body and footer (`app.go:324-326`). The user can't see which line they're commenting on while typing.

**Goal:** Render the text input inside the diff view at the cursor row, directly below the line being commented on.

### Changes

**`diffview.go` — Add an active comment input field:**
- Add fields to `diffView`: `commentActive bool`, `commentLineNum int`, `commentInput textinput.Model`
- In `renderSideBySide()` and `renderUnified()`, after rendering the cursor row, if `commentActive && row.lineNum == commentLineNum`, insert a rendered text input row styled with `commentPromptStyle`
- Add methods: `activateComment(lineNum int)`, `deactivateComment()`, `commentValue() string`
- The text input `Update()` should be called from within diffView, not app.go

**`comment.go` — Remove or repurpose:**
- The standalone `commentInput` struct is no longer needed as a separate bottom-bar component
- Either delete `comment.go` and move the textinput into `diffView`, or keep it as a reusable input widget that `diffView` embeds

**`app.go` — Update comment flow:**
- `updateNormal()` `keys.Comment` case (line 170): Instead of setting `m.mode = modeComment` and activating a bottom-bar input, call `m.diffView.activateComment(lineNum)` and set mode
- `updateComment()` (line 202): Forward key messages to `m.diffView.commentInput.Update(msg)` instead of `m.comment.input.Update(msg)`
- On `keys.Submit`: Read value from `m.diffView.commentValue()`, add to review, call `m.diffView.deactivateComment()` and `m.diffView.buildRows()`
- `View()` (line 324): Remove the special `modeComment` branch that inserts `m.comment.view()` between body and footer. The comment input is now rendered inside the diff view itself.

### Rendering Detail

In side-by-side mode, the inline input should span the full diff width (both columns), styled distinctly:

```
│  42 │  old code here        │  42 │  new code here          │
│  ┌─ Comment on line 42 ──────────────────────────────────┐  │
│  │ > user is typing here...                              │  │
│  └────────────────────────────────────────────────────────┘  │
│  43 │  next old line        │  43 │  next new line          │
```

---

## Refactor 2: General Comment Support

**Problem:** `review.Review.GeneralComment` exists and `formatter.go:57-59` renders it, but there's no UI to set it.

**Goal:** `G` key opens a comment input for review-level or file-level general comments (not attached to a specific line).

### Changes

**`keys.go` — Add key binding:**
```go
GeneralComment: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "general comment"))
```

**`app.go` — Handle `keys.GeneralComment`:**
- Set `m.mode = modeComment` with a special sentinel (e.g., `line = -1` or a separate `modeGeneralComment`)
- Activate an input. This one can stay at the bottom of the screen since it's not line-attached — reuse the existing bottom-bar `commentInput` for this purpose
- On submit: Set `m.review.GeneralComment = text` (or append if we want multiple general comments)

**`review.go` — Support multiple general comments:**
- Change `GeneralComment string` to `GeneralComments []string` (or use `Comment` with empty file/line 0)
- Update `output/formatter.go` to render all general comments

**`app.go` `View()` footer:**
- Update the footer keybinding hints to show `[G]eneral comment`

---

## Refactor 3: Comment Indicators in File List

**Problem:** After adding comments, there's no visual feedback in the sidebar showing which files have review notes.

**Goal:** Show a marker (e.g., `●` or comment count) next to files that have comments.

### Changes

**`filelist.go` — Accept review reference:**
- Change `fileList` to hold a `*review.Review` reference (set via `setReview()` or passed during construction)
- In `view()`, for each file, check `review.CommentsForFile(filename)`:
  - If comments exist, prepend a marker: `M ● auth.go +5 -2` or `M auth.go [3] +5 -2` (showing comment count)
- Use `colorYellow` for the indicator to match comment styling

**`app.go` — Wire review to file list:**
- After `newFileList(files)`, call `m.fileList.review = m.review`
- No other changes needed — the file list reads from the review on each render

---

## Refactor 4: Call `clampScroll()` After Layout Changes

**Problem:** `clampScroll()` (`diffview.go:120-145`) is defined but never called. After terminal resize or expand/collapse toggle, `scrollY` and `cursorY` can be out of bounds.

### Changes

**`app.go` — Add `clampScroll()` call in `updateLayout()`:**
```go
func (m *Model) updateLayout() {
    fileListWidth := 20
    if m.expanded {
        fileListWidth = 0
    }
    m.fileList.height = m.height - 4
    m.diffView.width = m.width - fileListWidth - 2
    m.diffView.height = m.height - 4
    m.diffView.clampScroll()  // <-- add this
}
```

This is a one-line fix.

---

## Implementation Order

1. **Refactor 4** (clampScroll) — One-line fix, no risk, do first
2. **Refactor 3** (comment indicators) — Small, isolated change to filelist.go
3. **Refactor 2** (general comments) — Adds a new feature, touches review model and formatter
4. **Refactor 1** (inline comment input) — Largest change, restructures comment rendering flow

Refactors 2-4 are independent and could be done in any order. Refactor 1 is the most involved and should be done last since it restructures how comment input works.

---

## Files Touched Per Refactor

| Refactor | Files Modified | Files Removed |
|----------|---------------|---------------|
| 1. Inline comments | `diffview.go`, `app.go`, `comment.go` (remove or gut), `styles.go` | Possibly `comment.go` |
| 2. General comments | `keys.go`, `app.go`, `review.go`, `formatter.go` | None |
| 3. Comment indicators | `filelist.go`, `app.go` | None |
| 4. clampScroll | `app.go` (one line) | None |
