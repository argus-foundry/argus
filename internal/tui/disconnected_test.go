package tui

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type unhealthyRunner struct{ agent.SessionProvider }

func (unhealthyRunner) SessionListHealthy() bool { return false }

func TestSessionListHealthy(t *testing.T) {
	testutil.Equal(t, sessionListHealthy(agent.NewRunner(nil)), true) // no signal → healthy
	testutil.Equal(t, sessionListHealthy(unhealthyRunner{}), false)
}

func TestApp_UpdateDisconnected(t *testing.T) {
	app, _ := newAppForStreamTest(t)
	app.tasks = []*model.Task{
		{ID: "dead", Status: model.StatusInProgress},
		{ID: "alive", Status: model.StatusInProgress},
		{ID: "done", Status: model.StatusComplete},
		{ID: "arch", Status: model.StatusInProgress, Archived: true},
		{ID: "review", Status: model.StatusInReview},
	}
	running := []string{"alive"}

	app.updateDisconnected(running, true) // tick 1: inside grace
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)

	app.updateDisconnected(running, false) // unreadable list: no new marks, counters kept
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)

	app.updateDisconnected(running, true) // tick 2: past grace
	testutil.DeepEqual(t, app.DisconnectedTaskIDs(), []string{"dead", "review"})

	app.updateDisconnected([]string{"alive", "dead", "review"}, true) // agents came back
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)
}
