package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	NextFile   key.Binding
	PrevFile   key.Binding
	NextCommit key.Binding
	PrevCommit key.Binding
	Comment    key.Binding
	Submit     key.Binding
	Cancel     key.Binding
	Expand     key.Binding
	ToggleView key.Binding
	Save       key.Binding
	Quit       key.Binding
	DeleteComment key.Binding
	HalfPageDown key.Binding
	HalfPageUp   key.Binding
}

var keys = keyMap{
	Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "scroll")),
	Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("", "")),
	NextFile:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n/N", "next/prev file")),
	PrevFile:   key.NewBinding(key.WithKeys("N"), key.WithHelp("", "")),
	NextCommit: key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("h/l", "prev/next commit")),
	PrevCommit: key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("", "")),
	Comment:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
	Submit:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	Cancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Expand:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "expand")),
	ToggleView: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "toggle view")),
	Save:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save review")),
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	DeleteComment: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete comment")),
	HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
	HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
}
