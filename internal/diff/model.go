package diff

// DiffLine represents a single line in a diff.
type LineOp int

const (
	OpEqual  LineOp = iota // unchanged context line
	OpDelete               // removed line (old side only)
	OpInsert               // added line (new side only)
)

type DiffLine struct {
	Op      LineOp
	Content string // line content without +/- prefix
	OldNum  int    // line number in old file (0 if not present)
	NewNum  int    // line number in new file (0 if not present)
}

// LinePair pairs old and new lines for side-by-side display.
type LinePair struct {
	Left  *DiffLine // nil if this is a pure insertion
	Right *DiffLine // nil if this is a pure deletion
}

type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Section  string // function/section header
	Lines    []DiffLine
	Pairs    []LinePair // aligned for side-by-side view
}

type FileDiff struct {
	OldName        string
	NewName        string
	Hunks          []Hunk
	Binary         bool
	Untracked      bool
	Staged         bool
	CollapsedCount int // >0 means this is a collapsed untracked directory summary
}
