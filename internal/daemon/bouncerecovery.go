package daemon

import (
	"log/slog"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bouncerecovery"
	"github.com/argus-foundry/argus/internal/db"
)

// bounceRecoveryRunner adapts the daemon's session runner to
// bouncerecovery.Runner. Resume reuses HeraReviveRunner.RestartDead, the same
// dead-session restart hera_revive and the REST resume endpoints use.
type bounceRecoveryRunner struct {
	revive *HeraReviveRunner
	runner agent.SessionRunner
}

var _ bouncerecovery.Runner = (*bounceRecoveryRunner)(nil)

func (r *bounceRecoveryRunner) IsAlive(taskID string) bool { return r.revive.IsAlive(taskID) }

func (r *bounceRecoveryRunner) Resume(taskID string) error { return r.revive.RestartDead(taskID) }

// Settled: the resumed session is alive and idle (the worker's own minimum
// age keeps a not-yet-started session from reading as settled).
func (r *bounceRecoveryRunner) Settled(taskID string) bool {
	sess := r.runner.Get(taskID)
	return sess != nil && sess.Alive() && sess.IsIdle()
}

// runBounceRecovery is the recovery worker goroutine body; d.done gates it.
func (d *Daemon) runBounceRecovery() {
	if d.notifier == nil {
		slog.Warn("[bounce-recovery] no notifier; recovery worker not started")
		return
	}
	var store *db.DB = d.db
	w := bouncerecovery.NewWorker(store,
		&bounceRecoveryRunner{revive: NewHeraReviveRunner(d.db, d.runner, d.cfgFn), runner: d.runner},
		d.notifier, d.cfgFn)
	// A resumed agent launched before the MCP port binds would start without
	// its --mcp-config and, being alive, never be restarted to get it. Wait for
	// the port unless the MCP server is disabled (it would never bind).
	w.Ready = func() bool {
		if !d.cfgFn().KB.Enabled {
			return true
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.mcpPort != 0
	}
	slog.Info("[bounce-recovery] worker starting")
	w.Run(d.done)
}
