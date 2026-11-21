package tui

import (
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/gdamore/tcell/v2"
)

// boardKindsApp seeds a coordinator board and returns a running app focused on
// it. It skips until the store (board-kinds change) accepts action/todo posts,
// so the same test activates itself once both branches are merged.
func boardKindsApp(t *testing.T) (*App, *db.DB, *db.HeraRole, func()) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	o, err := d.CreateHeraOrchestrator("kinds", "")
	testutil.NoError(t, err)
	c, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	if _, err := d.PostHeraBoardItem(c.ID, "action", "probe", "b"); err != nil {
		t.Skipf("store does not accept action/todo kinds yet: %v", err)
	}
	app := New(d, nil, false)
	sim, stop := wireApp(t, app)
	sim.SetSize(200, 50)
	sim.InjectKey(tcell.KeyRune, '2', 0)
	syncUI(t, app.tapp)
	readUI(t, app.tapp, func() { testutil.True(t, app.heraPage.SelectOrch(o.ID)) })
	return app, d, c, stop
}

func TestSmoke_BoardActionDoneAndCopyNotice(t *testing.T) {
	app, d, c, stop := boardKindsApp(t)
	defer stop()
	var mu sync.Mutex
	var copied string
	app.clipboardWriter = func(s string) error { mu.Lock(); copied = s; mu.Unlock(); return nil }
	act, err := d.PostHeraBoardItem(c.ID, "action", "Run it", "do", nil)
	testutil.NoError(t, err)
	_ = act
	app.heraPage.Refresh()
	syncUI(t, app.tapp)
	readUI(t, app.tapp, func() {
		app.heraPage.Machine().SetRegion(hera.FocusAgent)
		app.tapp.SetFocus(app.heraPage)
	})
	// Copy has no step on this item: a notice, never a clipboard write.
	app.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'c', 0))
	syncUI(t, app.tapp)
	mu.Lock()
	testutil.Equal(t, copied, "")
	mu.Unlock()
	app.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'd', 0))
	deadline := time.After(uiTimeout)
	for {
		items, err := d.ListHeraBoardItems(c.ID, false)
		testutil.NoError(t, err)
		open := 0
		for _, i := range items {
			if i.ID == act.ID {
				open++
			}
		}
		if open == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("action was not resolved by d")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
