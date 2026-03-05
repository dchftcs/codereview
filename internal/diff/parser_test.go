package diff

import "testing"

func TestParseEmptyDiff(t *testing.T) {
	t.Parallel()

	files, err := Parse("")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("len(files) = %d, want 0", len(files))
	}
}

func TestParseMultiFileAndLineNumbers(t *testing.T) {
	t.Parallel()

	raw := `diff --git a/a.txt b/a.txt
index 1111111..2222222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,4 @@
 alpha
-beta
+beta2
 gamma
+delta
\ No newline at end of file
diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
--- a/old.txt
+++ b/new.txt
@@ -1,1 +1,1 @@
-old
+new
`

	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}

	f0 := files[0]
	if f0.OldName != "a.txt" || f0.NewName != "a.txt" {
		t.Fatalf("unexpected file names: old=%q new=%q", f0.OldName, f0.NewName)
	}
	if len(f0.Hunks) != 1 {
		t.Fatalf("len(f0.Hunks) = %d, want 1", len(f0.Hunks))
	}

	h := f0.Hunks[0]
	if h.OldStart != 1 || h.OldCount != 3 || h.NewStart != 1 || h.NewCount != 4 {
		t.Fatalf("unexpected hunk header: %+v", h)
	}
	if got := len(h.Lines); got != 5 {
		t.Fatalf("len(h.Lines) = %d, want 5", got)
	}

	assertLine(t, h.Lines[0], OpEqual, "alpha", 1, 1)
	assertLine(t, h.Lines[1], OpDelete, "beta", 2, 0)
	assertLine(t, h.Lines[2], OpInsert, "beta2", 0, 2)
	assertLine(t, h.Lines[3], OpEqual, "gamma", 3, 3)
	assertLine(t, h.Lines[4], OpInsert, "delta", 0, 4)

	if got := len(h.Pairs); got != 4 {
		t.Fatalf("len(h.Pairs) = %d, want 4", got)
	}
	if h.Pairs[1].Left == nil || h.Pairs[1].Right == nil {
		t.Fatalf("expected modification pair at index 1, got %+v", h.Pairs[1])
	}

	f1 := files[1]
	if f1.OldName != "old.txt" || f1.NewName != "new.txt" {
		t.Fatalf("unexpected second file names: old=%q new=%q", f1.OldName, f1.NewName)
	}
}

func TestAlignPairsDeleteInsertSequences(t *testing.T) {
	t.Parallel()

	lines := []DiffLine{
		{Op: OpDelete, Content: "d1", OldNum: 10},
		{Op: OpDelete, Content: "d2", OldNum: 11},
		{Op: OpInsert, Content: "i1", NewNum: 10},
		{Op: OpEqual, Content: "ctx", OldNum: 12, NewNum: 11},
		{Op: OpInsert, Content: "tail", NewNum: 12},
	}

	pairs := alignPairs(lines)
	if got := len(pairs); got != 4 {
		t.Fatalf("len(pairs) = %d, want 4", got)
	}

	if pairs[0].Left == nil || pairs[0].Right == nil {
		t.Fatalf("pair 0 should be delete+insert, got %+v", pairs[0])
	}
	if pairs[1].Left == nil || pairs[1].Right != nil {
		t.Fatalf("pair 1 should be delete-only, got %+v", pairs[1])
	}
	if pairs[2].Left == nil || pairs[2].Right == nil || pairs[2].Left.Op != OpEqual || pairs[2].Right.Op != OpEqual {
		t.Fatalf("pair 2 should be equal/equal, got %+v", pairs[2])
	}
	if pairs[3].Left != nil || pairs[3].Right == nil || pairs[3].Right.Op != OpInsert {
		t.Fatalf("pair 3 should be insert-only, got %+v", pairs[3])
	}
}

func TestAlignPairsPureInsertsAndDeletes(t *testing.T) {
	t.Parallel()

	deleteOnly := alignPairs([]DiffLine{{Op: OpDelete, Content: "gone", OldNum: 5}})
	if len(deleteOnly) != 1 || deleteOnly[0].Left == nil || deleteOnly[0].Right != nil {
		t.Fatalf("delete-only alignment mismatch: %+v", deleteOnly)
	}

	insertOnly := alignPairs([]DiffLine{{Op: OpInsert, Content: "new", NewNum: 7}})
	if len(insertOnly) != 1 || insertOnly[0].Left != nil || insertOnly[0].Right == nil {
		t.Fatalf("insert-only alignment mismatch: %+v", insertOnly)
	}
}

func TestCleanPathStripsGitPrefixes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "a/pkg/file.go", want: "pkg/file.go"},
		{in: "b/pkg/file.go", want: "pkg/file.go"},
		{in: "pkg/file.go", want: "pkg/file.go"},
	}

	for _, tc := range cases {
		if got := cleanPath(tc.in); got != tc.want {
			t.Fatalf("cleanPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func assertLine(t *testing.T, line DiffLine, op LineOp, content string, oldNum, newNum int) {
	t.Helper()
	if line.Op != op || line.Content != content || line.OldNum != oldNum || line.NewNum != newNum {
		t.Fatalf("line mismatch: got %+v, want op=%v content=%q old=%d new=%d", line, op, content, oldNum, newNum)
	}
}
