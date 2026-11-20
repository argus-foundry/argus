package tui

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/gdamore/tcell/v2"
)

func TestSmoke_BoardDecideFYIAndEmpty(t *testing.T) {
	for _, hasFYI := range []bool{false, true} {
		name := "empty"
		if hasFYI {
			name = "fyi"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			d := testDB(t)
			o, err := d.CreateHeraOrchestrator("launch", "")
			testutil.NoError(t, err)
			c, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
			testutil.NoError(t, err)
			var focus int64
			if hasFYI {
				i, e := d.PostHeraBoardItem(c.ID, "fyi", "Notice", "Body")
				testutil.NoError(t, e)
				focus = i.ID
			}
			p := hera.NewHeraPage(d)
			p.Refresh()
			testutil.Equal(t, p.SelectOrch(o.ID), true)
			// Empty boards are explicitly opened with the same existing board toggle.
			p.Machine().SetRegion(hera.FocusAgent)
			app, _, _ := simApp(t)
			app.SetRoot(p, true).SetFocus(p)
			results := make(chan [2]int64, 1)
			p.OnBoardDecide = func(role, id int64) { results <- [2]int64{role, id} }
			stop := runApp(t, app)
			defer stop()
			if !hasFYI {
				app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'p', 0))
			}
			app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
			app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'o', 0))
			select {
			case got := <-results:
				testutil.DeepEqual(t, got, [2]int64{c.ID, focus})
			case <-time.After(uiTimeout):
				t.Fatal("browser launch callback missing")
			}
		})
	}
}
