package review

import "testing"

func TestReviewCommentLifecycle(t *testing.T) {
	t.Parallel()

	rev := New()
	rev.AddComment("a.go", 10, "first")
	rev.AddComment("a.go", 12, "second")
	rev.AddComment("b.go", 10, "third")

	if got := len(rev.Comments); got != 3 {
		t.Fatalf("len(Comments) = %d, want 3", got)
	}
	if rev.Comments[0].File != "a.go" || rev.Comments[0].Line != 10 || rev.Comments[0].Text != "first" {
		t.Fatalf("unexpected first comment: %+v", rev.Comments[0])
	}

	c := rev.FindComment("a.go", 12)
	if c == nil {
		t.Fatal("FindComment(a.go, 12) = nil, want comment")
	}
	if c.Text != "second" {
		t.Fatalf("FindComment text = %q, want %q", c.Text, "second")
	}

	if miss := rev.FindComment("missing.go", 1); miss != nil {
		t.Fatalf("FindComment(missing.go, 1) = %+v, want nil", miss)
	}

	aComments := rev.CommentsForFile("a.go")
	if got := len(aComments); got != 2 {
		t.Fatalf("len(CommentsForFile(a.go)) = %d, want 2", got)
	}
	if aComments[0].Line != 10 || aComments[1].Line != 12 {
		t.Fatalf("unexpected comments for a.go: %+v", aComments)
	}

	bComments := rev.CommentsForFile("b.go")
	if got := len(bComments); got != 1 || bComments[0].Text != "third" {
		t.Fatalf("unexpected comments for b.go: %+v", bComments)
	}
}

func TestDeleteCommentRemovesOnlyMatchingEntries(t *testing.T) {
	t.Parallel()

	rev := New()
	rev.AddComment("same.go", 7, "left")
	rev.AddComment("same.go", 7, "right")
	rev.AddComment("same.go", 8, "keep")
	rev.AddComment("other.go", 7, "keep other")

	rev.DeleteComment("same.go", 7)

	if got := len(rev.Comments); got != 2 {
		t.Fatalf("len(Comments) after delete = %d, want 2", got)
	}
	if rev.FindComment("same.go", 7) != nil {
		t.Fatal("FindComment(same.go, 7) still exists, want nil")
	}
	if rev.FindComment("same.go", 8) == nil {
		t.Fatal("FindComment(same.go, 8) = nil, want keep")
	}
	if rev.FindComment("other.go", 7) == nil {
		t.Fatal("FindComment(other.go, 7) = nil, want keep")
	}
}

func TestDeleteCommentMissingIsNoOp(t *testing.T) {
	t.Parallel()

	rev := New()
	rev.AddComment("x.go", 1, "x")
	rev.DeleteComment("x.go", 2)

	if got := len(rev.Comments); got != 1 {
		t.Fatalf("len(Comments) = %d, want 1", got)
	}
	if rev.Comments[0].Text != "x" {
		t.Fatalf("comment changed unexpectedly: %+v", rev.Comments[0])
	}
}

func TestAddGeneralCommentAppendsInOrder(t *testing.T) {
	t.Parallel()

	rev := New()
	rev.AddGeneralComment("first")
	rev.AddGeneralComment("second")

	if got := len(rev.GeneralComments); got != 2 {
		t.Fatalf("len(GeneralComments) = %d, want 2", got)
	}
	if rev.GeneralComments[0] != "first" || rev.GeneralComments[1] != "second" {
		t.Fatalf("unexpected GeneralComments order: %+v", rev.GeneralComments)
	}
}

func TestEmptyReviewBehaviors(t *testing.T) {
	t.Parallel()

	rev := New()
	if c := rev.FindComment("none.go", 1); c != nil {
		t.Fatalf("FindComment on empty review = %+v, want nil", c)
	}
	if got := len(rev.CommentsForFile("none.go")); got != 0 {
		t.Fatalf("len(CommentsForFile) = %d, want 0", got)
	}
	rev.DeleteComment("none.go", 1)
	if got := len(rev.Comments); got != 0 {
		t.Fatalf("len(Comments) = %d, want 0", got)
	}
}
