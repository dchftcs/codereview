package tui

import (
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type contextMenuItem struct {
	label  string
	action string // action identifier
}

type contextMenu struct {
	x, y     int // screen position to render at
	items    []contextMenuItem
	selected int
	// Context for the right-click location
	diffRow int    // the diff row that was right-clicked
	file    string // file context
	panel   string // "filelist" or "diff"
}

// handleRightClick opens a context menu at the click position.
func (m Model) handleRightClick(x, y int) (tea.Model, tea.Cmd) {
	headerH := lipgloss.Height(m.renderHeader())
	bodyY := y - headerH - 1

	if bodyY < 0 || bodyY >= m.diffView.height {
		return m, nil
	}

	// File list panel
	if x <= m.fileListWidth+1 {
		m.ctxMenu = contextMenu{
			x:     x,
			y:     y,
			panel: "filelist",
			items: []contextMenuItem{
				{label: "Toggle expand", action: "toggle_expand"},
			},
			selected: 0,
		}
		m.mode = modeContextMenu
		return m, nil
	}

	// Diff panel
	diffY := bodyY - 1 // subtract path line
	if diffY < 0 {
		return m, nil
	}
	row := m.diffView.scrollY + diffY
	if row < 0 || row >= len(m.diffView.rows) {
		return m, nil
	}

	file, _ := m.currentReviewFileName()
	items := []contextMenuItem{
		{label: "Add comment", action: "add_comment"},
		{label: "Copy line", action: "copy_line"},
	}

	m.ctxMenu = contextMenu{
		x:        x,
		y:        y,
		panel:    "diff",
		diffRow:  row,
		file:     file,
		items:    items,
		selected: 0,
	}
	m.mode = modeContextMenu
	return m, nil
}

// updateContextMenu handles keyboard input in context menu mode.
func (m Model) updateContextMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "j", "down":
		if m.ctxMenu.selected < len(m.ctxMenu.items)-1 {
			m.ctxMenu.selected++
		}
		return m, nil
	case "k", "up":
		if m.ctxMenu.selected > 0 {
			m.ctxMenu.selected--
		}
		return m, nil
	case "enter":
		return m.executeContextMenuItem()
	default:
		// Any other key dismisses
		m.mode = modeNormal
		return m, nil
	}
}

// updateContextMenuMouse handles mouse input in context menu mode.
func (m Model) updateContextMenuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	// Check if click is within the menu
	menuWidth, menuHeight := m.contextMenuDimensions()
	mx, my := m.ctxMenu.x, m.ctxMenu.y
	// Menu box has 1-char border on each side
	if msg.X >= mx && msg.X < mx+menuWidth && msg.Y >= my && msg.Y < my+menuHeight {
		// Map click to item index: border(1) + item rows
		itemIdx := msg.Y - my - 1 // subtract top border
		if itemIdx >= 0 && itemIdx < len(m.ctxMenu.items) {
			m.ctxMenu.selected = itemIdx
			return m.executeContextMenuItem()
		}
	}
	// Click outside dismisses
	m.mode = modeNormal
	return m, nil
}

// executeContextMenuItem runs the selected menu item's action.
func (m Model) executeContextMenuItem() (tea.Model, tea.Cmd) {
	if m.ctxMenu.selected < 0 || m.ctxMenu.selected >= len(m.ctxMenu.items) {
		m.mode = modeNormal
		return m, nil
	}
	item := m.ctxMenu.items[m.ctxMenu.selected]
	m.mode = modeNormal

	switch item.action {
	case "add_comment":
		if m.ctxMenu.file == "" {
			return m, nil
		}
		// Move cursor to the right-clicked row first
		m.diffView.moveCursorTo(m.ctxMenu.diffRow)
		lineNum := m.diffView.currentLineNum()
		if lineNum == 0 {
			return m, nil
		}
		m.mode = modeComment
		m.diffView.activateComment(m.ctxMenu.file, lineNum)
		return m, m.diffView.commentInput.Focus()

	case "copy_line":
		row := m.ctxMenu.diffRow
		if row < 0 || row >= len(m.diffView.rows) {
			return m, nil
		}
		r := m.diffView.rows[row]
		if r.kind != rowDiffPair || r.pair == nil {
			return m, nil
		}
		var text string
		if r.pair.Right != nil {
			text = r.pair.Right.Content
		} else if r.pair.Left != nil {
			text = r.pair.Left.Content
		}
		if text != "" {
			if err := clipboard.WriteAll(text); err != nil {
				m.statusMsg = "Copy failed: " + err.Error()
			} else {
				m.statusMsg = "Copied to clipboard"
			}
		}
		return m, nil

	case "toggle_expand":
		_, idx, ok := m.fileList.modifiedSelection()
		if !ok {
			return m, nil
		}
		m.expandedSet[idx] = !m.expandedSet[idx]
		if m.expandedSet[idx] {
			if m.expandedFiles != nil {
				m.setDiffViewForSelection(true)
			} else {
				return m, m.loadExpandedDiff()
			}
		} else {
			m.setDiffViewForSelection(true)
		}
		return m, nil
	}

	return m, nil
}

// contextMenuDimensions returns the total width and height of the rendered menu box.
func (m Model) contextMenuDimensions() (int, int) {
	maxLabel := 0
	for _, item := range m.ctxMenu.items {
		if len(item.label) > maxLabel {
			maxLabel = len(item.label)
		}
	}
	// width: border(1) + padding(1) + label + padding(1) + border(1)
	width := maxLabel + 4
	// height: border(1) + items + border(1)
	height := len(m.ctxMenu.items) + 2
	return width, height
}

// renderContextMenu returns the menu box as a string.
func (m Model) renderContextMenu() string {
	var rows []string
	for i, item := range m.ctxMenu.items {
		if i == m.ctxMenu.selected {
			rows = append(rows, contextMenuSelectedStyle.Render(item.label))
		} else {
			rows = append(rows, contextMenuItemStyle.Render(item.label))
		}
	}
	content := strings.Join(rows, "\n")
	return contextMenuBoxStyle.Render(content)
}

// overlayAt composites an overlay string on top of a base string at screen position (x, y).
func overlayAt(base, overlay string, x, y int) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	for oi, oLine := range overlayLines {
		baseIdx := y + oi
		if baseIdx < 0 || baseIdx >= len(baseLines) {
			continue
		}

		bLine := baseLines[baseIdx]
		// Work with runes for proper width handling, but we need ANSI-awareness.
		// Strip ANSI from base to get visible positions, then do character replacement.
		bRunes := []rune(stripAnsi(bLine))
		oRunes := []rune(stripAnsi(oLine))

		// Ensure the base line is wide enough
		for len(bRunes) < x+len(oRunes) {
			bRunes = append(bRunes, ' ')
		}

		// Replace characters at the overlay position
		for j, r := range oRunes {
			pos := x + j
			if pos >= 0 && pos < len(bRunes) {
				bRunes[pos] = r
			}
		}

		baseLines[baseIdx] = string(bRunes)
	}

	return strings.Join(baseLines, "\n")
}
