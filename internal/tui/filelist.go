package tui

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
)

type fileList struct {
	files    []diff.FileDiff
	selected int
	height   int
	offset   int // scroll offset
}

func newFileList(files []diff.FileDiff) fileList {
	return fileList{files: files}
}

func (fl *fileList) next() {
	if fl.selected < len(fl.files)-1 {
		fl.selected++
		fl.ensureVisible()
	}
}

func (fl *fileList) prev() {
	if fl.selected > 0 {
		fl.selected--
		fl.ensureVisible()
	}
}

func (fl *fileList) ensureVisible() {
	if fl.height <= 0 {
		return
	}
	if fl.selected < fl.offset {
		fl.offset = fl.selected
	}
	if fl.selected >= fl.offset+fl.height {
		fl.offset = fl.selected - fl.height + 1
	}
}

func (fl *fileList) selectedFile() *diff.FileDiff {
	if len(fl.files) == 0 {
		return nil
	}
	return &fl.files[fl.selected]
}

func (fl *fileList) view(width int) string {
	if len(fl.files) == 0 {
		return "No files"
	}

	var lines []string
	end := fl.offset + fl.height
	if end > len(fl.files) {
		end = len(fl.files)
	}

	for i := fl.offset; i < end; i++ {
		f := fl.files[i]
		name := filepath.Base(f.NewName)
		if f.NewName == "/dev/null" {
			name = filepath.Base(f.OldName)
		}

		// Count changes
		adds, dels := 0, 0
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				switch l.Op {
				case diff.OpInsert:
					adds++
				case diff.OpDelete:
					dels++
				}
			}
		}

		indicator := "M"
		if f.OldName == "/dev/null" {
			indicator = "A"
		} else if f.NewName == "/dev/null" {
			indicator = "D"
		}

		stat := fmt.Sprintf(" +%d -%d", adds, dels)
		label := fmt.Sprintf("%s %s%s", indicator, name, stat)

		if i == fl.selected {
			label = selectedFileStyle.Width(width - 2).Render(label)
		} else {
			label = normalFileStyle.Width(width - 2).Render(label)
		}
		lines = append(lines, label)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return fileListStyle.Width(width).Height(fl.height).Render(content)
}
