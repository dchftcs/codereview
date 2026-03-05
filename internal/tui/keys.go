package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up             key.Binding
	Down           key.Binding
	NextFile       key.Binding
	PrevFile       key.Binding
	NextCommit     key.Binding
	PrevCommit     key.Binding
	Comment        key.Binding
	Submit         key.Binding
	Cancel         key.Binding
	Expand         key.Binding
	ToggleView     key.Binding
	Save           key.Binding
	Quit           key.Binding
	DeleteComment  key.Binding
	EditComment    key.Binding
	GeneralComment key.Binding
	HalfPageDown   key.Binding
	HalfPageUp     key.Binding
	Help           key.Binding
	Search         key.Binding
	SearchNext     key.Binding
	SearchPrev     key.Binding
	FileSearch     key.Binding
	ContentSearch  key.Binding
	ToggleFileTree key.Binding
	ToggleTreeDir  key.Binding
	NextModified   key.Binding
	PrevModified   key.Binding
	FirstModified  key.Binding
}

var keys = keyMap{
	Up:             key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "scroll")),
	Down:           key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("", "")),
	NextFile:       key.NewBinding(key.WithKeys("]"), key.WithHelp("]/[", "next/prev file")),
	PrevFile:       key.NewBinding(key.WithKeys("["), key.WithHelp("", "")),
	NextCommit:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("h/l", "prev/next commit")),
	PrevCommit:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("", "")),
	Comment:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
	Submit:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	Cancel:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Expand:         key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "expand")),
	ToggleView:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "toggle view")),
	Save:           key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save review")),
	Quit:           key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	DeleteComment:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete comment")),
	EditComment:    key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit comment")),
	GeneralComment: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "general comment")),
	HalfPageDown:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
	HalfPageUp:     key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
	Help:           key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Search:         key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	SearchNext:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
	SearchPrev:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
	FileSearch:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "find file")),
	ContentSearch:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "find content")),
	ToggleFileTree: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle file tree")),
	ToggleTreeDir:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open/close dir")),
	NextModified:   key.NewBinding(key.WithKeys("}"), key.WithHelp("}/{", "next/prev modified")),
	PrevModified:   key.NewBinding(key.WithKeys("{"), key.WithHelp("", "")),
	FirstModified:  key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "first modified")),
}
