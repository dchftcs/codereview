package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
)

type commentInput struct {
	input   textinput.Model
	active  bool
	file    string
	line    int
}

func newCommentInput() commentInput {
	ti := textinput.New()
	ti.Placeholder = "Enter comment..."
	ti.CharLimit = 500
	ti.Width = 60
	return commentInput{input: ti}
}

func (ci *commentInput) activate(file string, line int) {
	ci.active = true
	ci.file = file
	ci.line = line
	ci.input.SetValue("")
	ci.input.Focus()
}

func (ci *commentInput) deactivate() {
	ci.active = false
	ci.input.Blur()
}

func (ci *commentInput) value() string {
	return ci.input.Value()
}

func (ci *commentInput) view() string {
	if !ci.active {
		return ""
	}
	return commentPromptStyle.Render("Comment: ") + ci.input.View()
}
