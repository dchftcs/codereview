# Load Review Plan

## Goals

1. Add reliable save/load for reviews.
2. Make diff provenance explicit in both saved data and UI.
3. Handle changed git state safely, especially for uncommitted/branch-diff workflows.
4. Avoid silent data loss when comments cannot be remapped.

## Design Summary

Use a dual-output model:

- **Machine format (authoritative):** `.crreview` JSON for save/load round-tripping.
- **Human/agent format:** existing Markdown output for reading/LLM workflows.

Loading works via a **mapping confidence model**:

- `exact`: same target location.
- `shifted`: remapped with anchor/context heuristics.
- `unresolved`: cannot safely place.

Unresolved comments are preserved and visible; never dropped silently.

## Review File Format (`.crreview`)

Top-level fields:

- `format_version`
- `created_at`
- `tool_version`
- `source`
- `repo_fingerprint`
- `comments`
- `general_comments`

### `source` metadata

- `kind`: `commit` | `range` | `branch_diff` | `working_tree`
- `rev_spec`: example `main...HEAD`, `abc..def`, `HEAD~1`
- `head_at_save`
- `base_at_save` (merge-base for branch diffs when available)
- `diff_fingerprint` (hash of canonicalized full diff)
- optional per-file fingerprints for improved remap quality

### Comment payload extensions

Keep existing fields and add optional anchors for remap:

- `file`
- `line`
- `text`
- `side` (`old`/`new`, optional)
- `context_before` (small window)
- `context_after` (small window)
- `line_fingerprint` (optional hash of line + neighbors)

## Provenance and UI

Display source and match status clearly:

- Loaded source kind/rev-spec.
- Saved HEAD/base vs current HEAD/base.
- Match summary: `exact`, `shifted`, `unresolved` counts.

Example status text:

- `Loaded review: branch_diff main...HEAD`
- `Saved HEAD abc1234, current HEAD def5678`
- `Mapped: 10 exact, 3 shifted, 1 unresolved`

## Load Behavior

### Step 1: Source comparison

On load, compare saved source info with current repo state.

- If `diff_fingerprint` matches: direct mapping path.
- If mismatch: prompt/flow with explicit warning.

### Step 2: Mapping pipeline

1. **Exact pass**
   - Match by file + line (+ side if stored).
   - Prefer when source/diff fingerprints match.
2. **Anchor remap pass**
   - Use stored context windows and/or line fingerprints.
   - Find best candidate in current file.
3. **Unresolved pass**
   - Keep comments that cannot be safely placed.
   - Surface in unresolved list/panel and in status counts.

### Step 3: Safety policy

- Never auto-discard comments.
- Mark confidence for every imported comment.
- If repository mismatch is large, allow:
  - `attempt remap`
  - `load unresolved only`
  - `cancel`

## Uncommitted Review Strategy

`working_tree` and `branch_diff` are volatile and expected to drift.

Policy:

- Supported by default.
- Always persist source metadata + diff fingerprint.
- On load with mismatch, show warning and confidence-based mapping results.

This keeps the workflow flexible without pretending mapping is always exact.

## CLI and UX Changes

1. Add `--load <path>` to load `.crreview`.
2. Keep `-o` for markdown export.
3. Add save path for `.crreview` (explicit command/key flow), or include both outputs when saving.
4. Optional later: `--import-md` as best-effort lossy import.

## Phased Implementation

### Phase 1: Foundation

- Define `.crreview` schema and serialization.
- Capture source metadata + diff fingerprint at review creation/save.
- Add explicit provenance display in UI.
- Add basic load with strict/exact mapping only.
- Preserve unmappable comments as unresolved.

### Phase 2: Robust remapping

- Store per-comment anchors/context.
- Implement anchor-based remap pass.
- Add confidence classification (`exact`/`shifted`/`unresolved`).
- Add load summary UI.

### Phase 3: UX polish

- Add unresolved comment inspection/workflow.
- Add mismatch prompts and action choices.
- Improve status/header messaging.

### Phase 4: Optional imports

- Add markdown import (best-effort) if needed.
- Keep `.crreview` as canonical source of truth.

## Risks and Guardrails

- **Risk:** False-positive remap onto wrong line.
  - Guardrail: conservative matching + confidence labels + unresolved fallback.
- **Risk:** User assumes loaded comments are exact.
  - Guardrail: explicit match summary in UI.
- **Risk:** Format drift over time.
  - Guardrail: `format_version` + migration path.

## Acceptance Criteria

1. A saved `.crreview` can be reloaded with all comments retained.
2. Source provenance is visible and specific.
3. Mismatch states are detected and communicated.
4. Unmapped comments remain accessible (not lost).
5. Loading in changed working tree is supported with explicit confidence outcomes.
