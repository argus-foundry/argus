package modal

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func press(m *AccountListModal, r rune) {
	m.InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, 0), func(p tview.Primitive) {})
}

func listModal() *AccountListModal {
	return NewAccountListModal("Accounts", "auto", []AccountListEntry{
		{Name: "default", Checked: true},
		{Name: "personal"},
		{Name: "work"},
		{Name: "auto", Label: "<auto>"},
	})
}

func names(m *AccountListModal) []string {
	var out []string
	for _, e := range m.Entries() {
		out = append(out, e.Name)
	}
	return out
}

func TestAccountList_ToggleOrdersByCheckTime(t *testing.T) {
	m := listModal()
	testutil.DeepEqual(t, m.Selected(), []string{"default"})
	m.cursor = 1 // personal
	press(m, ' ')
	m.cursor = 3 // work (index shifted: default, personal, work, auto)
	testutil.Equal(t, m.Entries()[2].Name, "work")
	m.cursor = 2
	press(m, ' ')
	testutil.DeepEqual(t, m.Selected(), []string{"default", "personal", "work"})
	// Unchecking removes it from the order.
	m.cursor = 1
	press(m, ' ')
	testutil.DeepEqual(t, m.Selected(), []string{"default", "work"})
}

func TestAccountList_ReorderKeepsAutoLast(t *testing.T) {
	m := listModal()
	m.cursor = 1
	press(m, ' ') // personal checked
	m.cursor = 3
	press(m, ' ') // auto checked -> sorts to the end of the checked block
	testutil.DeepEqual(t, m.Selected(), []string{"default", "personal", "auto"})
	m.cursor = 1 // personal
	press(m, 'K')
	testutil.DeepEqual(t, m.Selected(), []string{"personal", "default", "auto"})
	testutil.Equal(t, m.Cursor(), 0)
	// Moving down onto auto is refused.
	m.cursor = 1
	press(m, 'J')
	testutil.DeepEqual(t, m.Selected(), []string{"personal", "default", "auto"})
	// auto itself cannot move up.
	m.cursor = 2
	press(m, 'K')
	testutil.DeepEqual(t, m.Selected(), []string{"personal", "default", "auto"})
	// Unchecked rows do not reorder.
	m.cursor = 3
	press(m, 'K')
	testutil.DeepEqual(t, names(m), []string{"personal", "default", "auto", "work"})
}

func TestAccountList_SaveCancelAndCursorBounds(t *testing.T) {
	m := listModal()
	press(m, 'k')
	testutil.Equal(t, m.Cursor(), 0)
	for range 10 {
		press(m, 'j')
	}
	testutil.Equal(t, m.Cursor(), 3)
	m.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) {})
	testutil.True(t, m.Saved())
	c := listModal()
	c.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) {})
	testutil.True(t, c.Canceled())
	testutil.False(t, c.Saved())
}

func TestAccountList_Draw(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, sim.Init())
	defer sim.Fini()
	sim.SetSize(80, 20)
	m := listModal()
	m.SetRect(0, 0, 80, 20)
	m.Draw(sim)
	sim.Show()
	cells, w, h := sim.GetContents()
	var text []rune
	for i := 0; i < w*h; i++ {
		if len(cells[i].Runes) > 0 {
			text = append(text, cells[i].Runes[0])
		}
	}
	s := string(text)
	for _, want := range []string{"[x] 1 default", "[ ]", "<auto>", "Space toggle"} {
		testutil.Contains(t, s, want)
	}
}
