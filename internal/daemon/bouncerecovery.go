package daemon

import (
	"log/slog"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bouncerecovery"
	"github.com/argus-foundry/argus/internal/model"
)

// bounceRecoveryRunner adapts the daemon's session runner to
// bouncerecovery.Runner. Resume uses the resume-only variant of the shared
// dead-session restart; explicit user revives may still recover fresh.
type bounceRecoveryRunner struct {
	revive   *HeraReviveRunner
	runner   agent.SessionRunner
	attempts map[string]uint64
}

var _ bouncerecovery.Runner = (*bounceRecoveryRunner)(nil)

func (r *bounceRecoveryRunner) IsAlive(taskID string) bool { return r.revive.IsAlive(taskID) }

type startReceipts interface {
	StartReceipt(string) (uint64, int, bool)
}

func (r *bounceRecoveryRunner) Resume(taskID string) error {
	return r.revive.restartDead(taskID, true, func(seq uint64) {
		if r.attempts == nil {
			r.attempts = map[string]uint64{}
		}
		r.attempts[taskID] = seq
	})
}

// OwnsLiveSession requires the successful receipt for this exact Start and PID;
// neither a timeout alone nor an arbitrary live process is enough evidence.
func (r *bounceRecoveryRunner) OwnsLiveSession(taskID string) bool {
	receipts, ok := r.runner.(startReceipts)
	if !ok || r.attempts[taskID] == 0 {
		return false
	}
	seq, pid, _ := receipts.StartReceipt(taskID)
	sess := r.runner.Get(taskID)
	if seq != r.attempts[taskID] || pid <= 0 || sess == nil || !sess.Alive() || sess.PID() != pid {
		return false
	}
	if err := r.revive.database.SetStatus(taskID, model.StatusInProgress); err != nil {
		slog.Warn("[bounce-recovery] confirmed start status write failed", "task", taskID, "err", err)
		return false
	}
	return true
}

// ResumePending prevents retries from replacing the receipt while the original
// RPC is still starting the agent in the supervisor.
func (r *bounceRecoveryRunner) ResumePending(taskID string) bool {
	receipts, ok := r.runner.(startReceipts)
	if !ok || r.attempts[taskID] == 0 {
		return false
	}
	seq, _, pending := receipts.StartReceipt(taskID)
	return seq == r.attempts[taskID] && pending
}

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
	w := bouncerecovery.NewWorker(d.db,
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
