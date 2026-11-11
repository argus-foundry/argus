package bouncerecovery

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
)

// settleCeiling is how long a resumed session counts against MaxConcurrent
// even if it never settles.
const settleCeiling = 60 * time.Second

// minSettle is the earliest a resumed session can count as settled: a session
// with no output yet also reads as idle, so a floor avoids releasing the
// concurrency slot before the agent has actually started.
const minSettle = 8 * time.Second

// noteDeadlineMS gives the idle-gated note delivery longer than the default,
// since a freshly resumed agent can take a while to reach its first idle.
const noteDeadlineMS = 10 * 60 * 1000

var retryBackoff = []time.Duration{10 * time.Second, 30 * time.Second}

// TaskStore is the DB surface the worker needs (satisfied by *db.DB).
type TaskStore interface {
	Store
	Get(taskID string) (*model.Task, error)
}

// Runner is the session seam: liveness, start, and settle detection.
type Runner interface {
	IsAlive(taskID string) bool
	// Resume starts (resuming the conversation) a session with no live process.
	Resume(taskID string) error
	// Settled reports a resumed session has produced output and gone idle.
	Settled(taskID string) bool
}

// Notifier delivers text to a task's session as an idle-gated turn.
type Notifier interface {
	ReliableNotify(taskID, text, deliveryID string, opts notify.NotifyOpts) func()
}

// Worker drives pending ledger entries through resume, throttled.
type Worker struct {
	store  TaskStore
	runner Runner
	notify Notifier
	cfgFn  func() config.Config
	now    func() time.Time

	// Ready, when set, gates every tick: the daemon passes "MCP port bound"
	// so a resumed agent is never launched without its --mcp-config.
	Ready func() bool

	inflight  map[string]time.Time
	lastStart time.Time
}

// NewWorker builds a Worker.
func NewWorker(store TaskStore, runner Runner, n Notifier, cfgFn func() config.Config) *Worker {
	return &Worker{store: store, runner: runner, notify: n, cfgFn: cfgFn, now: nowFn, inflight: map[string]time.Time{}}
}

// Run ticks until done closes. Blocks; call in a goroutine.
func (w *Worker) Run(done <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			w.Tick()
		}
	}
}

// Tick runs one pass: expire, re-evaluate delegation, and start at most one
// resume when the throttle allows.
func (w *Worker) Tick() {
	cfg := w.cfgFn().BounceRecovery
	if !cfg.Enabled || (w.Ready != nil && !w.Ready()) {
		return
	}
	entries, err := LoadAll(w.store)
	if err != nil {
		slog.Warn("[bounce-recovery] ledger read failed", "err", err)
		return
	}
	if len(entries) == 0 {
		return
	}
	now := w.now()
	w.reevaluate(entries, now, cfg)

	for id, started := range w.inflight {
		if age := now.Sub(started); (age >= minSettle && w.runner.Settled(id)) || age >= settleCeiling {
			delete(w.inflight, id)
		}
	}
	if len(w.inflight) >= max(cfg.MaxConcurrent, 1) {
		return
	}
	if !w.lastStart.IsZero() && now.Sub(w.lastStart) < time.Duration(cfg.StaggerSeconds)*time.Second {
		return
	}
	e := nextPending(entries, now)
	if e == nil {
		return
	}
	w.resume(e, entries, now)
}

// reevaluate expires stale entries and promotes delegated workers whose
// coordinator will not be resumed.
func (w *Worker) reevaluate(entries map[string]*Entry, now time.Time, cfg config.BounceRecoveryConfig) {
	expiry := time.Duration(cfg.ExpiryHours) * time.Hour
	for _, e := range entries {
		switch e.Status {
		case StatusPending, StatusDelegated:
			if expiry > 0 && now.Sub(e.KilledAt) > expiry {
				slog.Info("[bounce-recovery] entry expired", "task", e.TaskID, "age", now.Sub(e.KilledAt).Round(time.Minute))
				w.mark(e, StatusExpired)
			}
		}
	}
	for _, e := range entries {
		if e.Status != StatusDelegated {
			continue
		}
		c := entries[e.CoordTask]
		if c != nil && (c.Status == StatusPending || c.Status == StatusResumed) {
			continue
		}
		next := unsupervisedStatus(*e)
		slog.Info("[bounce-recovery] coordinator will not resume; re-evaluating worker", "task", e.TaskID, "coord", e.CoordTask, "status", next)
		w.mark(e, next)
	}
}

func (w *Worker) mark(e *Entry, st Status) {
	if err := SetStatus(w.store, e, st); err != nil {
		slog.Warn("[bounce-recovery] ledger status write failed", "task", e.TaskID, "status", st, "err", err)
	}
}

// nextPending picks the next eligible entry: coordinators first by depth, then
// the rest, oldest kill first.
func nextPending(entries map[string]*Entry, now time.Time) *Entry {
	var cands []*Entry
	for _, e := range entries {
		if e.Status == StatusPending && !now.Before(e.NextAt) {
			cands = append(cands, e)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if (a.Role == RoleCoordinator) != (b.Role == RoleCoordinator) {
			return a.Role == RoleCoordinator
		}
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		if !a.KilledAt.Equal(b.KilledAt) {
			return a.KilledAt.Before(b.KilledAt)
		}
		return a.TaskID < b.TaskID
	})
	return cands[0]
}

func (w *Worker) resume(e *Entry, entries map[string]*Entry, now time.Time) {
	// Re-check reality right before acting: a human may have gotten there first.
	t, err := w.store.Get(e.TaskID)
	switch {
	case err != nil || t == nil || t.Archived || t.Status == model.StatusComplete:
		slog.Info("[bounce-recovery] skip: task gone, archived or complete", "task", e.TaskID, "err", err)
		w.mark(e, StatusSuperseded)
		return
	case w.runner.IsAlive(e.TaskID) || t.Status == model.StatusInProgress:
		slog.Info("[bounce-recovery] skip: already resumed by someone else", "task", e.TaskID, "status", t.Status)
		w.mark(e, StatusSuperseded)
		return
	}

	e.Attempts++
	w.lastStart = now
	if err := w.runner.Resume(e.TaskID); err != nil {
		if e.Attempts >= MaxAttempts {
			slog.Error("[bounce-recovery] resume failed, giving up", "task", e.TaskID, "attempts", e.Attempts, "err", err)
			w.mark(e, StatusFailed)
			return
		}
		back := retryBackoff[min(e.Attempts-1, len(retryBackoff)-1)]
		e.NextAt = now.Add(back)
		slog.Warn("[bounce-recovery] resume failed, will retry", "task", e.TaskID, "attempt", e.Attempts, "retry_in", back, "err", err)
		w.mark(e, StatusPending)
		return
	}
	e.NextAt = time.Time{}
	w.mark(e, StatusResumed)
	w.inflight[e.TaskID] = now

	var team []*Entry
	if e.Role == RoleCoordinator {
		for _, m := range entries {
			if m.CoordTask == e.TaskID && m.TaskID != e.TaskID {
				team = append(team, m)
			}
		}
	}
	note := BuildNote(e, team)
	w.notify.ReliableNotify(e.TaskID, note, fmt.Sprintf("bounce-recovery:%s:%s", e.TaskID, e.BounceID), notify.NotifyOpts{DeadlineMS: noteDeadlineMS})
	slog.Info("[bounce-recovery] resumed", "task", e.TaskID, "name", e.TaskName, "role", e.Role, "state", e.State, "team", len(team), "attempt", e.Attempts)
}
