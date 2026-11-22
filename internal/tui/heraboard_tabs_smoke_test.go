package tui

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/gdamore/tcell/v2"
)

func TestSmoke_BoardKindTabArrows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	o, err := d.CreateHeraOrchestrator("tabs", "")
	testutil.NoError(t, err)
	c, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	decision, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "Body")
	testutil.NoError(t, err)
	fyi, err := d.PostHeraBoardItem(c.ID, "fyi", "Notice", "Body")
	testutil.NoError(t, err)
	p := hera.NewHeraPage(d)
	p.Refresh()
	testutil.True(t, p.SelectOrch(o.ID))
	p.Machine().SetRegion(hera.FocusAgent)
	app, _, _ := simApp(t)
	app.SetRoot(p, true).SetFocus(p)
	selected := make(chan int64, 4)
	p.OnBoardDecide = func(_, id int64) { selected <- id }
	stop := runApp(t, app)
	defer stop()
	check := func(key tcell.Key, want int64) {
		t.Helper()
		app.QueueEvent(tcell.NewEventKey(key, 0, 0))
		app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'o', 0))
		select {
		case id := <-selected:
			testutil.Equal(t, id, want)
		case <-time.After(uiTimeout):
			t.Fatal("board tab event did not arrive")
		}
	}
	check(tcell.KeyLeft, decision.ID) // clamped first tab
	check(tcell.KeyRight, fyi.ID)
	check(tcell.KeyLeft, decision.ID)
	// A rebind must replace, rather than supplement, the physical arrow.
	keys, warnings := keymap.Build(config.Keybindings{HeraBoard: map[string]string{"tab_next": "v"}})
	testutil.Equal(t, len(warnings), 0)
	readUI(t, app, func() { p.Keys = func() *keymap.Keymap { return keys } })
	check(tcell.KeyRight, decision.ID)
	app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'v', 0))
	app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'o', 0))
	select {
	case id := <-selected:
		testutil.Equal(t, id, fyi.ID)
	case <-time.After(uiTimeout):
		t.Fatal("rebound tab did not arrive")
	}
}
