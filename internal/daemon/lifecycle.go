package daemon

import (
	"fmt"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/app/agentview"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/hera"
)

type runnerSessions struct{ runner agent.SessionProvider }

func (r runnerSessions) SessionAlive(taskID string) bool {
	sess := r.runner.Get(taskID)
	return sess != nil && sess.Alive()
}

func (r runnerSessions) SendSystemInput(taskID, text string) error {
	sess := r.runner.Get(taskID)
	if sess == nil {
		return fmt.Errorf("no live session for task %s", taskID)
	}
	_, err := sess.WriteInput([]byte(text), agentview.OriginSystem)
	return err
}

// NewHeraLifecycleService builds the worker-bounce / coordinator-force-recycle
// service on the daemon's real runner; the daemon and the local TUI share it.
func NewHeraLifecycleService(database *db.DB, runner agent.SessionRunner, cfgFn func() config.Config) *hera.LifecycleService {
	return hera.NewLifecycleService(database, runnerSessions{runner}, NewHeraRecycleRunner(database, runner, cfgFn))
}
