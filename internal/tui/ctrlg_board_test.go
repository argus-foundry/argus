package tui

import (
	"strconv"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
)

func TestSmoke_CtrlGUnreadBoard(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			d := testDB(t)
			oid := seedHeraOrch(t, d, "board-jump")
			c := seedHeraBoundRole(t, d, oid, "coord", db.HeraKindCoordinator, "tc")
			read, err := d.PostHeraBoardItem(c.ID, "fyi", "Already read", "Previous update")
			testutil.NoError(t, err)
			testutil.NoError(t, d.ReadHeraBoardItem(c.ID, read.ID))
			first, err := d.PostHeraBoardItem(c.ID, "decision", "Release approval needed", "Choose the release")
			testutil.NoError(t, err)
			second, err := d.PostHeraBoardItem(c.ID, "decision", "Second decision", "Another choice")
			testutil.NoError(t, err)
			testutil.NoError(t, d.SetConfigValue("ui.coordinator_board", strconv.FormatBool(enabled)))
			app := New(d, agent.NewRunner(nil), false)
			sim, stop := wireApp(t, app)
			defer stop()
			sim.SetSize(160, 50)
			readUI(t, app.tapp, func() { app.heraPage.Refresh() })
			// A queued sentinel is a FIFO barrier: QueueUpdate alone can overtake
			// SimulationScreen key delivery on a loaded race/coverage runner.
			processed := make(chan struct{}, 1)
			readUI(t, app.tapp, func() {
				capture := app.tapp.GetInputCapture()
				app.tapp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyF12 {
						processed <- struct{}{}
						return nil
					}
					return capture(event)
				})
			})
			queueKeys := func(keys ...*tcell.EventKey) {
				t.Helper()
				for _, key := range keys {
					app.tapp.QueueEvent(key)
				}
				app.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyF12, 0, 0))
				select {
				case <-processed:
				case <-time.After(5 * time.Second):
					t.Fatal("expected queued board keys to drain; FIFO sentinel was not processed")
				}
			}
			queueKeys(tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))
			readUI(t, app.tapp, func() {
				if !enabled {
					testutil.Equal(t, app.header.ActiveTab(), widget.TabTasks)
					return
				}
				testutil.Equal(t, app.header.ActiveTab(), widget.TabHera)
				testutil.Equal(t, app.heraPage.SelectionContext().FocusTaskID(), "tc")
				testutil.Nil(t, app.heraPage.SelectionContext().Role)
				testutil.Equal(t, app.heraPage.Machine().State(), hera.FocusAgent)
				// Assert rendered board content: selection alone could pass with
				// the details region still showing its plan instead of the board.
				w, h := sim.Size()
				testutil.Contains(t, screenText(sim, w, h), "Release approval needed")
			})
			items, err := d.ListHeraBoardItems(c.ID, false)
			testutil.NoError(t, err)
			testutil.True(t, items[0].Read)
			testutil.False(t, items[1].Read)
			testutil.False(t, items[2].Read)
			if !enabled {
				return
			}
			// Observe the existing acknowledgment seam synchronously: landing
			// and j/k must not acknowledge; Enter must expand the selected item.
			var expanded []int64
			readUI(t, app.tapp, func() {
				app.heraPage.OnBoardRead = func(_ int64, id int64, _ time.Time) {
					expanded = append(expanded, id)
				}
			})
			queueKeys(tcell.NewEventKey(tcell.KeyRune, 'j', 0), tcell.NewEventKey(tcell.KeyEnter, 0, 0))
			readUI(t, app.tapp, func() { testutil.DeepEqual(t, expanded, []int64{second.ID}) })
			queueKeys(tcell.NewEventKey(tcell.KeyRune, 'k', 0), tcell.NewEventKey(tcell.KeyEnter, 0, 0))
			readUI(t, app.tapp, func() { testutil.DeepEqual(t, expanded, []int64{second.ID, first.ID}) })
		})
	}
}
