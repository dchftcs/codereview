package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
)

// bottomBarInput is a reusable text input rendered at the bottom of the screen.
// Used for general comments and other prompts not attached to a specific diff line.
type bottomBarInput struct {
	input  textinput.Model
	active bool
	label  string
}

func newBottomBarInput() bottomBarInput {
	ti := textinput.New()
	ti.Placeholder = "Enter comment..."
	ti.CharLimit = 500
	ti.Width = 60
	return bottomBarInput{input: ti}
}

func (b *bottomBarInput) activate(label string) {
	b.active = true
	b.label = label
	b.input.SetValue("")
	b.input.Focus()
}

func (b *bottomBarInput) deactivate() {
	b.active = false
	b.input.Blur()
}

func (b *bottomBarInput) value() string {
	return b.input.Value()
}

func (b *bottomBarInput) view() string {
	if !b.active {
		return ""
	}
	return commentPromptStyle.Render(b.label+" ") + b.input.View()
}
