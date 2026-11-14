package daemon

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/lastexchange"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// graduationWatcher has no expiry or force path. Membership and restart phase
// live in SQLite; reconstructing this watcher after a bounce loses no intent.
type graduationWatcher struct {
	db        *db.DB
	runner    agent.SessionRunner
	cfgFn     func() config.Config
	notifier  graduationNotifier
	resumable func(*model.Task, config.Config) (bool, error)
	stopJobs  func(*model.Task, config.Config, string) error
	stop      chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	deferred  map[string]string
	// now and lastResume rate-limit dead-session resume attempts so a session
	// that dies immediately is not restarted on every one-second sweep.
	now        func() time.Time
	lastResume map[string]time.Time
	backoffLog map[string]bool
	// lastReason and preflightFail dedupe the deferral log line per distinct
	// reason and rate-limit the preflight itself (ResolveBackend plus the
	// transcript probe) to once per backoff window while it keeps failing.
	lastReason    map[string]string
	preflightFail map[string]time.Time
	logf          func(string, ...any)
}

// errGraduationBackoff is the silent "preflight failed recently, not yet"
// outcome: the sweep neither logs nor re-runs the preflight for it.
var errGraduationBackoff = errors.New("graduation preflight backoff")

const graduationResumeBackoff = 10 * time.Second

type graduationNotifier interface {
	ReliableNotify(string, string, string, notify.NotifyOpts) func()
	DeliveryState(string, string) notify.DeliveryState
}

func newGraduationWatcher(d *db.DB, r agent.SessionRunner, cfg func() config.Config, n graduationNotifier) *graduationWatcher {
	return &graduationWatcher{db: d, runner: r, cfgFn: cfg, notifier: n, resumable: agent.ResumableSessionAvailable, stopJobs: agent.StopStrayJobs, stop: make(chan struct{}), done: make(chan struct{}), deferred: make(map[string]string), now: time.Now, lastResume: make(map[string]time.Time), backoffLog: make(map[string]bool), lastReason: make(map[string]string), preflightFail: make(map[string]time.Time), logf: uxlog.Log}
}

func (g *graduationWatcher) start() {
	defer close(g.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	g.sweep()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			g.sweep()
		}
	}
}

func (g *graduationWatcher) preflight(t *model.Task) error {
	backend, err := agent.ResolveBackend(t, g.cfgFn())
	if err != nil || !agent.IsClaudeBackend(backend.Command) {
		return fmt.Errorf("only Claude workers can graduate (Codex is ineligible)")
	}
	if t.SessionID == "" {
		return fmt.Errorf("graduation requires a resumable session id")
	}
	if g.runner.HasPendingRestart(t.ID) {
		return fmt.Errorf("graduation refused: restart already pending")
	}
	if agent.AccountMoveInFlight(t.ID) {
		return fmt.Errorf("graduation refused: an account or backend move of this worker is in progress")
	}
	if sess := g.runner.Get(t.ID); sess != nil && sess.Alive() && !sess.IsIdle() {
		return fmt.Errorf("graduation refused: session is busy")
	}
	ok, err := g.resumable(t, g.cfgFn())
	if err != nil {
		return fmt.Errorf("graduation refused: resumable transcript unavailable: %w", err)
	}
	if !ok {
		return fmt.Errorf("graduation refused: resumable transcript unavailable")
	}
	return nil
}

func (g *graduationWatcher) sweep() {
	g.mu.Lock()
	defer g.mu.Unlock()
	meta, err := g.db.ListMetaByNamespace(db.HeraMetaNamespace)
	if err != nil {
		uxlog.Log("[resident] graduation poll failed: %v", err)
		return
	}
	for taskID, kv := range meta {
		if phase := kv[db.HeraMetaKeyGraduationPendingRestart]; phase != "" {
			switch err := g.advance(taskID, phase); {
			case err == nil:
				delete(g.lastReason, taskID)
			case errors.Is(err, errGraduationBackoff):
			case g.lastReason[taskID] != err.Error():
				g.lastReason[taskID] = err.Error()
				g.logf("[resident] graduation task=%s deferred: %v", taskID, err)
			}
		} else if kv["graduation_orientation"] == "" {
			g.forget(taskID)
		}
		if body := kv["graduation_orientation"]; body != "" && g.notifier != nil {
			id := "graduation:" + taskID
			g.notifier.ReliableNotify(taskID, body, id, notify.NotifyOpts{})
			if g.notifier.DeliveryState(taskID, id) == notify.StateSubmitted {
				if err := g.db.SetMeta(taskID, db.HeraMetaNamespace, "graduation_orientation", ""); err != nil {
					uxlog.Log("[resident] graduation orientation ack task=%s failed: %v", taskID, err)
				}
			}
		}
	}
}

// forget drops every per-task memo once the pending marker is gone.
func (g *graduationWatcher) forget(taskID string) {
	delete(g.lastReason, taskID)
	delete(g.preflightFail, taskID)
	delete(g.deferred, taskID)
	delete(g.lastResume, taskID)
	delete(g.backoffLog, taskID)
}

// resolveGraduate finds the graduate's coordinator binding via the binding id
// recorded at graduation (a task holding a second live binding would make the
// by-task lookup ambiguous forever). A recorded binding that has ended, or no
// longer exists, abandons the pending state instead of retrying forever. A
// pending row WITHOUT a recorded id is never guessed at (graduation always
// records one): it is cleared with a log line, which also keeps a human
// abandon from being resurrected by a sweep that read its snapshot earlier.
func (g *graduationWatcher) resolveGraduate(taskID string) (b *db.HeraBinding, abandoned bool, err error) {
	meta, err := g.db.ListMetaByNamespace(db.HeraMetaNamespace)
	if err != nil {
		return nil, false, err
	}
	raw := meta[taskID][db.HeraMetaKeyGraduationBindingID]
	if raw == "" {
		if aerr := g.db.ClearGraduationState(taskID); aerr != nil {
			return nil, false, aerr
		}
		uxlog.Log("[resident] graduation cleared: pending state has no recorded binding id task=%s", taskID)
		g.forget(taskID)
		return nil, true, nil
	}
	id, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil {
		return nil, false, fmt.Errorf("graduation binding id %q: %w", raw, perr)
	}
	b, err = g.db.HeraBindingByID(id)
	if errors.Is(err, db.ErrHeraNotFound) || (err == nil && b.EndedAt != nil) {
		if aerr := g.db.ClearGraduationState(taskID); aerr != nil {
			return nil, false, aerr
		}
		uxlog.Log("[resident] graduation abandoned: graduate binding ended task=%s", taskID)
		delete(g.deferred, taskID)
		return nil, true, nil
	}
	return b, false, err
}

func (g *graduationWatcher) advance(taskID, phase string) error {
	t, err := g.db.Get(taskID)
	if err != nil {
		return err
	}
	b, abandoned, err := g.resolveGraduate(taskID)
	if err != nil {
		return err
	}
	if abandoned {
		return nil
	}
	r, err := g.db.HeraRole(b.RoleID)
	if err != nil {
		return err
	}
	if r.Kind != db.HeraKindCoordinator || t.IsResident() {
		return fmt.Errorf("graduate coordinator binding unavailable")
	}
	if g.runner.HasPendingRestart(taskID) {
		return nil
	}
	sess := g.runner.Get(taskID)
	alive := sess != nil && sess.Alive()
	// Read idleness first: for a daemon-client RemoteSession this refreshes the
	// cached PID, so the restart-complete comparison below sees the live PID
	// rather than the pre-kick one (a stale PID caused a second restart).
	idle := alive && sess.IsIdle()
	if strings.HasPrefix(phase, "restart:") && alive {
		pid, err := strconv.Atoi(strings.TrimPrefix(phase, "restart:"))
		if err != nil {
			return err
		}
		if sess.PID() != pid {
			o, err := g.db.HeraOrchestrator(b.OrchestratorID)
			if err != nil {
				return err
			}
			body := lastexchange.SystemTurnPrefix + "You were graduated from the resident's team. You are now a top-level coordinator. The resident is not your coordinator and cannot message you. Report to Aaron through the normal channels and dispatch work with hera_spawn_worker.\n\n" + agent.HeraCoordinatorOrientation(o.Name)
			// Compare-and-set on the observed phase with targeted PID/status
			// writes: a human abandon (+archive/delete) since this sweep's
			// snapshot must not be overwritten from stale row fields.
			done, err := g.db.CompleteHeraGraduationAt(taskID, phase, body, sess.PID())
			if err != nil {
				return err
			}
			if !done {
				uxlog.Log("[resident] graduation task=%s completion skipped: pending marker changed or cleared", taskID)
				g.forget(taskID)
				return nil
			}
			uxlog.Log("[resident] graduation restart complete task=%s session=%s", taskID, t.SessionID)
			return nil
		}
	}
	// Never clean up jobs or signal an active worker, however long it takes.
	if alive && !idle {
		if g.deferred[taskID] != "busy" {
			uxlog.Log("[resident] graduation task=%s waiting for idle; no deadline", taskID)
			g.deferred[taskID] = "busy"
		}
		return nil
	}
	delete(g.deferred, taskID)
	if !alive {
		if last, ok := g.lastResume[taskID]; ok && g.now().Sub(last) < graduationResumeBackoff {
			if !g.backoffLog[taskID] {
				uxlog.Log("[resident] graduation task=%s resume backoff %s after a recent attempt", taskID, graduationResumeBackoff)
				g.backoffLog[taskID] = true
			}
			return nil
		}
	}
	if last, ok := g.preflightFail[taskID]; ok && g.now().Sub(last) < graduationResumeBackoff {
		return errGraduationBackoff
	}
	if err := g.preflight(t); err != nil {
		g.preflightFail[taskID] = g.now()
		return err
	}
	delete(g.preflightFail, taskID)
	pid := 0
	if alive {
		pid = sess.PID()
	}
	// Compare-and-set: a human abandon between the sweep's snapshot and this
	// write removed the marker; never re-create it.
	ok, err := g.db.AdvanceGraduationPhase(taskID, phase, fmt.Sprintf("restart:%d", pid))
	if err != nil {
		return err
	}
	if !ok {
		uxlog.Log("[resident] graduation task=%s advance skipped: pending marker changed or cleared", taskID)
		g.forget(taskID)
		return nil
	}
	if err := g.stopJobs(t, g.cfgFn(), t.SessionID); err != nil {
		return err
	}
	if still, perr := g.db.GraduationPendingErr(taskID); perr != nil || !still {
		uxlog.Log("[resident] graduation task=%s kick skipped: pending cleared during job cleanup (err=%v)", taskID, perr)
		return perr
	}
	// Recheck after external cleanup. KickRerender preserves this exact session
	// ID and refuses missing transcripts; KickRevive could silently start fresh.
	if alive {
		if !sess.IsIdle() {
			return nil
		}
		cols, rows := sess.PTYSize()
		err = g.runner.KickRerender(t, g.cfgFn(), uint16(rows), uint16(cols)) //nolint:gosec // terminal cell counts
	} else {
		g.lastResume[taskID] = g.now()
		delete(g.backoffLog, taskID)
		_, _, err = g.runner.StartOrReattach(t, g.cfgFn(), 24, 80, true)
	}
	uxlog.Log("[resident] graduation resume task=%s session=%s alive=%t err=%v", taskID, t.SessionID, alive, err)
	if err == nil {
		return g.db.SetStatus(taskID, model.StatusInProgress)
	}
	return err
}
