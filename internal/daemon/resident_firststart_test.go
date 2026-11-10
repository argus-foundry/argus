package daemon

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// startProbeRunner snapshots team state at the instant the resident session
// would launch.
type startProbeRunner struct {
	*fakeResRunner
	onStart func(*model.Task)
}

func (r *startProbeRunner) StartOrReattach(t *model.Task, cfg config.Config, rows, cols uint16, resume bool) (agent.SessionHandle, bool, error) {
	r.onStart(t)
	return r.fakeResRunner.StartOrReattach(t, cfg, rows, cols, resume)
}

// Spec "First start creates the team": the standing orchestrator must exist and
// be bound to the resident task BEFORE the session launches. Fails if the tick's
// EnsureResidentOrchestrator call is removed (orchestrator lookup returns not found).
func TestResidentFirstStartCreatesTeamBeforeLaunch(t *testing.T) {
	e := newResEnv(t)
	started := false
	probe := &startProbeRunner{fakeResRunner: e.run}
	probe.onStart = func(task *model.Task) {
		started = true
		orch, err := e.db.HeraOrchestratorByName(db.ResidentOrchestratorName)
		testutil.NoError(t, err)
		b, err := e.db.HeraLiveBindingByTaskAndOrchestrator(task.ID, orch.ID)
		testutil.NoError(t, err)
		testutil.True(t, b != nil)
		testutil.Equal(t, b.ArgusTaskID, task.ID)
	}
	e.m = newResidentManager(e.db, probe, func() config.Config { return e.cfg })
	e.m.now = func() time.Time { return e.now }
	e.m.tick()
	testutil.True(t, started)
}
