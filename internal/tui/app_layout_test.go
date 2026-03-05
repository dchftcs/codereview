package tui

import "testing"

func TestUpdateLayoutEnforcesMinimumContentHeight(t *testing.T) {
	t.Parallel()

	m := NewModel(Config{})
	m.width = 100
	m.height = 8

	m.updateLayout()

	if m.diffView.height != minContentHeight {
		t.Fatalf("diffView.height = %d, want %d", m.diffView.height, minContentHeight)
	}
	if m.fileList.height != minContentHeight {
		t.Fatalf("fileList.height = %d, want %d", m.fileList.height, minContentHeight)
	}
}
