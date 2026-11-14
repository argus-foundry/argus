package modal

import (
	"github.com/argus-foundry/argus/internal/tui/theme"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// AccountListEntry is one row of the AccountListModal. Name is the stored
// value ("auto" for the any-account entry); Label replaces it on screen.
type AccountListEntry struct {
	Name    string
	Label   string
	Detail  string
	Checked bool
}

// AccountListModal edits an ordered multi-select list of accounts. Checked
// entries come first, in preference order; an entry named AutoName always
// sorts last among them. Up/Down (j/k) move the cursor, Space toggles, K/J
// reorder the entry under the cursor within the checked block, Enter saves,
// Esc / Ctrl+Q cancels. It has no text input, so no PasteHandler.
type AccountListModal struct {
	*tview.Box
	title    string
	autoName string
	entries  []AccountListEntry
	cursor   int
	saved    bool
	canceled bool
}

// NewAccountListModal builds the editor. autoName is the entry that must stay
// last among the checked ones ("" for none).
func NewAccountListModal(title, autoName string, entries []AccountListEntry) *AccountListModal {
	m := &AccountListModal{Box: tview.NewBox(), title: title, autoName: autoName, entries: append([]AccountListEntry(nil), entries...)}
	m.normalize()
	return m
}

// normalize stably partitions the entries: checked (auto last), then unchecked.
func (m *AccountListModal) normalize() {
	var checked, auto, rest []AccountListEntry
	for _, e := range m.entries {
		switch {
		case e.Checked && m.autoName != "" && e.Name == m.autoName:
			auto = append(auto, e)
		case e.Checked:
			checked = append(checked, e)
		default:
			rest = append(rest, e)
		}
	}
	m.entries = append(append(checked, auto...), rest...)
}

// Saved reports whether Enter confirmed the list; Canceled whether it was dismissed.
func (m *AccountListModal) Saved() bool    { return m.saved }
func (m *AccountListModal) Canceled() bool { return m.canceled }

// Selected returns the checked names in preference order.
func (m *AccountListModal) Selected() []string {
	var out []string
	for _, e := range m.entries {
		if e.Checked {
			out = append(out, e.Name)
		}
	}
	return out
}

// Entries returns the rows in display order (test/inspection seam).
func (m *AccountListModal) Entries() []AccountListEntry {
	return append([]AccountListEntry(nil), m.entries...)
}

// Cursor returns the highlighted row index.
func (m *AccountListModal) Cursor() int { return m.cursor }

func (m *AccountListModal) isAuto(i int) bool {
	return m.autoName != "" && m.entries[i].Name == m.autoName
}

func (m *AccountListModal) toggle() {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return
	}
	name := m.entries[m.cursor].Name
	m.entries[m.cursor].Checked = !m.entries[m.cursor].Checked
	m.normalize()
	for i, e := range m.entries {
		if e.Name == name {
			m.cursor = i
		}
	}
}

// move swaps the cursor entry with its neighbour inside the checked block
// (never past the auto entry).
func (m *AccountListModal) move(delta int) {
	i, j := m.cursor, m.cursor+delta
	if i < 0 || j < 0 || j >= len(m.entries) || !m.entries[i].Checked || !m.entries[j].Checked || m.isAuto(i) || m.isAuto(j) {
		return
	}
	m.entries[i], m.entries[j] = m.entries[j], m.entries[i]
	m.cursor = j
}

// InputHandler handles key events for the editor.
func (m *AccountListModal) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return m.WrapInputHandler(func(event *tcell.EventKey, _ func(p tview.Primitive)) {
		switch event.Key() {
		case tcell.KeyEnter:
			m.saved = true
		case tcell.KeyEscape, tcell.KeyCtrlQ:
			m.canceled = true
		case tcell.KeyUp:
			m.cursor = max(0, m.cursor-1)
		case tcell.KeyDown:
			m.cursor = max(0, min(m.cursor+1, len(m.entries)-1))
		case tcell.KeyRune:
			switch event.Rune() {
			case 'k':
				m.cursor = max(0, m.cursor-1)
			case 'j':
				m.cursor = max(0, min(m.cursor+1, len(m.entries)-1))
			case ' ':
				m.toggle()
			case 'K':
				m.move(-1)
			case 'J':
				m.move(1)
			}
		}
	})
}

// Draw renders the editor centered, covering its full panel rect.
func (m *AccountListModal) Draw(screen tcell.Screen) {
	m.DrawForSubclass(screen, m)
	x, y, width, height := m.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	formW := min(72, width-4)
	if formW < 12 {
		formW = width
	}
	chrome := 6
	shown := m.entries
	if maxRows := max(height-chrome, 0); len(shown) > maxRows {
		shown = shown[:maxRows]
	}
	formH := min(chrome+len(shown), height)
	formX := x + (width-formW)/2
	formY := max(y+(height-formH)/2, y)
	for row := formY; row < formY+formH && row < y+height; row++ {
		for col := formX; col < formX+formW && col < x+width; col++ {
			screen.SetContent(col, row, ' ', nil, tcell.StyleDefault)
		}
	}
	widget.DrawBorder(screen, formX, formY, formW, formH, theme.StyleFocusedBorder)
	widget.DrawText(screen, formX+2, formY+1, formW-4, m.title, theme.StyleTitle)
	pos := 0
	for i, e := range shown {
		style := theme.StyleNormal
		prefix := "  "
		if i == m.cursor {
			style = theme.StyleSelected
			prefix = "❯ "
		}
		box := "[ ]   "
		if e.Checked {
			pos++
			box = "[x] " + string(rune('0'+min(pos, 9))) + " "
		}
		label := e.Name
		if e.Label != "" {
			label = e.Label
		}
		if e.Detail != "" {
			label += "  " + e.Detail
		}
		widget.DrawText(screen, formX+2, formY+3+i, formW-4, prefix+box+label, style)
	}
	widget.DrawText(screen, formX+2, formY+formH-2, formW-4, "Space toggle  K/J reorder  Enter save  Esc cancel", theme.StyleDimmed)
}
