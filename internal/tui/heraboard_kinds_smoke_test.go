package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/gdamore/tcell/v2"
)

type boardCompletionSink struct{ completed chan int64 }

func (s boardCompletionSink) Emit(e model.Event) {
	if e.Type != model.EventTypeHeraBoardChanged {
		return
	}
	var p struct {
		ItemID    int64  `json:"item_id"`
		Operation string `json:"operation"`
	}
	if json.Unmarshal(e.Payload, &p) == nil && p.Operation == "resolved" {
		select {
		case s.completed <- p.ItemID:
		default:
		}
	}
}

func TestSmoke_BoardActionClearAndCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	o, err := d.CreateHeraOrchestrator("kinds", "")
	testutil.NoError(t, err)
	c, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	blocking, command := true, "make  migrate **now**"
	act, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "Run it", "Human step", db.HeraBoardMetadataPatch{Blocking: &blocking, Command: &command})
	testutil.NoError(t, err)
	app := New(d, agent.NewRunner(nil), false)
	copied := make(chan string, 1)
	app.clipboardWriter = func(s string) error { copied <- s; return nil }
	sim, stop := wireApp(t, app)
	defer stop()
	sim.SetSize(200, 50)
	app.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, '2', 0))
	syncUI(t, app.tapp)
	readUI(t, app.tapp, func() {
		testutil.True(t, app.heraPage.SelectOrch(o.ID))
		app.heraPage.Machine().SetRegion(hera.FocusAgent)
		app.tapp.SetFocus(app.heraPage)
	})
	app.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'c', 0))
	select {
	case got := <-copied:
		testutil.Equal(t, got, command)
	case <-time.After(uiTimeout):
		t.Fatal("copy key did not copy the exact step")
	}
	completed := make(chan int64, 1)
	previous := events.SetSink(boardCompletionSink{completed: completed})
	defer events.SetSink(previous)
	app.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'x', 0))
	select {
	case id := <-completed:
		testutil.Equal(t, id, act.ID)
	case <-time.After(uiTimeout):
		t.Fatal("clear key did not resolve action")
	}
	syncUI(t, app.tapp)
	items, err := d.ListHeraBoardItems(c.ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, items[0].Resolved, true)
	testutil.Equal(t, items[0].ResolutionChoice, "done")
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
}
