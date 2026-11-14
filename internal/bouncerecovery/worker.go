package bouncerecovery

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/uxlog"
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

// maxNoteQueues bounds how many times one note is re-queued before it is
// abandoned (each queue lives noteDeadlineMS).
const maxNoteQueues = 3

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
	// Submitted reports a delivery actually went out.
	Submitted(taskID, deliveryID string) bool
}

type queuedNote struct {
	at    time.Time
	count int
}

// Worker drives pending ledger entries through resume, throttled.
type Worker struct {
	store  TaskStore
	runner Runner
	notify Notifier
	cfgFn  func() config.Config
	now    func() time.Time

	// Ready, when set, gates starting resumes: the daemon passes "MCP port
	// bound" so a resumed agent is never launched without its --mcp-config.
	Ready func() bool

	inflight      map[string]time.Time
	queued        map[string]queuedNote // delivery ID → in-memory queue state
	lastStart     time.Time
	loggedWaiting bool
}

// NewWorker builds a Worker.
func NewWorker(store TaskStore, runner Runner, n Notifier, cfgFn func() config.Config) *Worker {
	return &Worker{store: store, runner: runner, notify: n, cfgFn: cfgFn, now: nowFn,
		inflight: map[string]time.Time{}, queued: map[string]queuedNote{}}
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

// DeliveryID is the stable notify delivery ID of an entry's resume note, so a
// re-queue after a daemon bounce dedups against an already-submitted note.
func DeliveryID(e *Entry) string {
	return fmt.Sprintf("bounce-recovery:%s:%s", e.TaskID, e.BounceID)
}

// Tick runs one pass: expire and prune, re-evaluate delegation, deliver owed
// notes, and start at most one resume when the throttle allows.
func (w *Worker) Tick() {
	cfg := w.cfgFn().BounceRecovery
	if !cfg.Enabled {
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
	// Expiry and pruning run BEFORE the readiness gate so a daemon that never
	// gets an MCP port cannot resurrect stale entries or keep them forever.
	w.expireAndPrune(entries, now, cfg)
	if w.Ready != nil && !w.Ready() {
		if !w.loggedWaiting {
			w.loggedWaiting = true
			slog.Info("[bounce-recovery] waiting for the MCP port before resuming agents")
			uxlog.Log("[bounce-recovery] waiting for the MCP port before resuming agents")
		}
		return
	}
	w.loggedWaiting = false
	w.reevaluate(entries)
	w.deliverNotes(entries, now)

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

// expireAndPrune expires stale actionable entries and deletes finished ones
// older than the expiry window, so LoadAll stays tiny.
func (w *Worker) expireAndPrune(entries map[string]*Entry, now time.Time, cfg config.BounceRecoveryConfig) {
	expiry := time.Duration(cfg.ExpiryHours) * time.Hour
	if expiry <= 0 {
		return
	}
	for id, e := range entries {
		age := now.Sub(e.KilledAt)
		if age <= expiry {
			continue
		}
		switch e.Status {
		case StatusPending, StatusDelegated:
			slog.Info("[bounce-recovery] entry expired", "task", e.TaskID, "age", age.Round(time.Minute))
			w.mark(e, StatusExpired)
		default:
			if n, err := w.store.DeleteMetaNamespaceForTask(id, Namespace); err != nil {
				slog.Warn("[bounce-recovery] prune failed", "task", id, "err", err)
			} else {
				slog.Info("[bounce-recovery] pruned finished entry", "task", id, "status", e.Status, "rows", n)
				delete(entries, id)
			}
		}
	}
}

// reevaluate promotes delegated workers whose coordinator will not be resumed,
// and supersedes them when the user abandoned that coordinator.
func (w *Worker) reevaluate(entries map[string]*Entry) {
	for _, e := range entries {
		if e.Status != StatusDelegated {
			continue
		}
		c := entries[e.CoordTask]
		if c != nil && (c.Status == StatusPending || c.Status == StatusResumed) {
			continue
		}
		if coordinatorClosedStore(w.store, e.CoordTask) {
			slog.Info("[bounce-recovery] coordinator archived or completed; not resuming its worker", "task", e.TaskID, "coord", e.CoordTask)
			e.Reason = "coordinator_closed"
			_ = w.store.SetMeta(e.TaskID, Namespace, "reason", e.Reason)
			w.mark(e, StatusSuperseded)
			continue
		}
		next := unsupervisedStatus(*e)
		slog.Info("[bounce-recovery] coordinator will not resume; re-evaluating worker", "task", e.TaskID, "coord", e.CoordTask, "status", next)
		w.mark(e, next)
	}
}

func coordinatorClosedStore(s TaskStore, coordTask string) bool {
	if coordTask == "" {
		return false
	}
	t, err := s.Get(coordTask)
	if err != nil {
		return false // a transient read error must not abandon a team
	}
	return t == nil || t.Archived || t.Status == model.StatusComplete
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
	meta, metaErr := w.store.ListMetaByNamespace("hera")
	if metaErr != nil {
		slog.Warn("[bounce-recovery] close-out check failed; deferring", "task", e.TaskID, "err", metaErr)
		return
	}
	pendingRPC := false
	if pending, ok := w.runner.(interface{ ResumePending(string) bool }); ok {
		pendingRPC = pending.ResumePending(e.TaskID)
	}
	switch {
	case err != nil || t == nil || t.Archived || t.Status == model.StatusComplete || meta[e.TaskID]["ready_to_close"] == "true":
		slog.Info("[bounce-recovery] skip: task gone, archived or complete", "task", e.TaskID, "err", err)
		w.mark(e, StatusSuperseded)
		return
	case pendingRPC:
		e.NextAt = now.Add(retryBackoff[0])
		slog.Info("[bounce-recovery] prior start still pending; deferring retry", "task", e.TaskID)
		w.mark(e, StatusPending)
		return
	case w.runner.IsAlive(e.TaskID) || t.Status == model.StatusInProgress:
		if owned, ok := w.runner.(interface{ OwnsLiveSession(string) bool }); ok && e.Attempts > 0 && owned.OwnsLiveSession(e.TaskID) {
			slog.Info("[bounce-recovery] prior start confirmed alive; delivering owed note", "task", e.TaskID)
			uxlog.Log("[bounce-recovery] prior start confirmed task=%s", e.TaskID)
			w.resumed(e, entries, now)
			return
		}
		slog.Info("[bounce-recovery] skip: already resumed by someone else", "task", e.TaskID, "status", t.Status)
		w.mark(e, StatusSuperseded)
		return
	}

	e.Attempts++
	w.lastStart = now
	if err := w.runner.Resume(e.TaskID); err != nil {
		if errors.Is(err, agent.ErrResumeUnavailable) {
			e.Reason = "no_transcript"
			_ = w.store.SetMeta(e.TaskID, Namespace, "reason", e.Reason)
			slog.Warn("[bounce-recovery] refusing fresh launch: conversation unavailable", "task", e.TaskID, "err", err)
			uxlog.Log("[bounce-recovery] refusing fresh launch task=%s: %v", e.TaskID, err)
			w.mark(e, StatusFailed)
			return
		}
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
	w.resumed(e, entries, now)
}

func (w *Worker) resumed(e *Entry, entries map[string]*Entry, now time.Time) {
	e.NextAt = time.Time{}
	e.NoteState = ""
	w.mark(e, StatusResumed)
	w.inflight[e.TaskID] = now
	slog.Info("[bounce-recovery] resumed", "task", e.TaskID, "name", e.TaskName, "role", e.Role, "state", e.State, "attempt", e.Attempts)
	// The note is queued by deliverNotes on the same tick-loop that re-queues it
	// after a daemon bounce, so there is exactly one delivery path.
	w.deliverNotes(entries, now)
}

// deliverNotes drives every resumed-but-un-noted entry's note to delivery. The
// state is persisted: an entry is "noted" only once the notifier reports the
// delivery submitted, and after a daemon restart (fresh in-memory queue) the
// note is re-queued under the SAME delivery ID.
func (w *Worker) deliverNotes(entries map[string]*Entry, now time.Time) {
	for _, e := range entries {
		if e.Status != StatusResumed || e.NoteState != "" {
			continue
		}
		did := DeliveryID(e)
		if w.notify.Submitted(e.TaskID, did) {
			if err := SetNoteState(w.store, e, NoteSent); err != nil {
				slog.Warn("[bounce-recovery] note state write failed", "task", e.TaskID, "err", err)
			}
			delete(w.queued, did)
			slog.Info("[bounce-recovery] note delivered", "task", e.TaskID)
			continue
		}
		q, ok := w.queued[did]
		if ok && now.Sub(q.at) < time.Duration(noteDeadlineMS)*time.Millisecond {
			continue // still pending in the notifier
		}
		if ok && q.count >= maxNoteQueues {
			slog.Warn("[bounce-recovery] note never delivered; abandoning", "task", e.TaskID, "queues", q.count)
			uxlog.Log("[bounce-recovery] resume note for %s never delivered", e.TaskID)
			_ = SetNoteState(w.store, e, NoteAbandoned)
			delete(w.queued, did)
			continue
		}
		var team []*Entry
		if e.Role == RoleCoordinator {
			for _, m := range entries {
				if m.CoordTask == e.TaskID && m.TaskID != e.TaskID {
					team = append(team, m)
				}
			}
		}
		w.notify.ReliableNotify(e.TaskID, BuildNote(e, team), did, notify.NotifyOpts{DeadlineMS: noteDeadlineMS})
		w.queued[did] = queuedNote{at: now, count: q.count + 1}
		slog.Info("[bounce-recovery] note queued", "task", e.TaskID, "queue", q.count+1, "team", len(team))
	}
}
