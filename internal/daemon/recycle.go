package daemon

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// HeraRecycleRunner implements hera.RecycleRunner against the daemon's real
// session runner and DB (add-coordinator-context-management D5). It is the
// seam recycle_test.go's fakeRecycleRunner stands in for in unit tests — this
// is the concrete, production version wired into the daemon's background
// recycle sweep and (Stage 7) the rail's human-forced keybinding via the
// exported NewHeraRecycleRunner (internal/tui/heraactions.go builds one
// directly since the TUI's hera mutation layer already operates on *db.DB +
// agent.SessionRunner in local mode, same pattern as its other hera ops).
type HeraRecycleRunner struct {
	database         *db.DB
	runner           agent.SessionRunner
	cfgFn            func() config.Config
	idle             agent.ContentIdleTracker
	now              func() time.Time
	residentRecycler *taskRecycleRunner
}

// NewHeraRecycleRunner builds the production hera.RecycleRunner. Exported so
// both the daemon's background RecycleWatcher and the TUI's human-forced `B`
// rail keybinding share the identical kill/restart implementation rather than
// each hand-rolling their own.
func NewHeraRecycleRunner(database *db.DB, runner agent.SessionRunner, cfgFn func() config.Config) *HeraRecycleRunner {
	return &HeraRecycleRunner{database: database, runner: runner, cfgFn: cfgFn, now: time.Now}
}

var _ hera.RecycleRunner = (*HeraRecycleRunner)(nil)

// IsIdle reports whether taskID's live session is currently idle. A missing
// session (already exited, or never started) is treated as idle — nothing is
// actively producing output, so a self-service recycle should proceed rather
// than wait forever for a session that no longer exists.
func (r *HeraRecycleRunner) IsIdle(taskID string) bool {
	sess := r.runner.Get(taskID)
	if sess == nil {
		r.idle.Forget(taskID)
		return true
	}
	return r.idle.IsIdle(taskID, sess, r.now())
}

// ResetIdle drops content-idle history when RecycleWatcher finishes or
// abandons a pending request, so a later request starts a fresh stability
// interval.
func (r *HeraRecycleRunner) ResetIdle(taskID string) { r.idle.Forget(taskID) }

// StopStrayJobs cleans up any Claude Code background job tied to sessionID
// before the caller restarts taskID (design.md Risks: task_stop does not kill
// everything).
func (r *HeraRecycleRunner) StopStrayJobs(taskID, sessionID string) error {
	task, err := r.database.Get(taskID)
	if err != nil {
		return fmt.Errorf("stop stray jobs: load task %s: %w", taskID, err)
	}
	if task.IsResident() {
		return nil // the shared resident runner owns cleanup under its guard
	}
	return agent.StopStrayJobs(task, r.cfgFn(), sessionID)
}

// Restart assembles the fresh session's seed prompt (mission + plan-DAG
// state + handoff note) for the already-resolved roleID, clears any stale
// SessionID so BuildCmd starts genuinely fresh rather than colliding on an
// already-used UUID, persists both, and hands off to the runner's same-task
// recycle primitive. roleID is resolved directly via HeraLiveBindingByRole —
// never re-derived from taskID alone — because a task holding 2+ live
// bindings (e.g. a worker in one orchestrator and a coordinator in another)
// would make a task-keyed lookup (HeraLiveBindingByTask) ambiguous.
func (r *HeraRecycleRunner) Restart(taskID string, roleID int64) error {
	task, err := r.database.Get(taskID)
	if err != nil {
		return fmt.Errorf("recycle restart: load task %s: %w", taskID, err)
	}
	if task.IsResident() {
		if r.residentRecycler == nil {
			return fmt.Errorf("resident recycle requires the daemon's shared runner")
		}
		note := ""
		if entries, err := r.database.ListMeta(taskID, db.HeraMetaNamespace); err == nil {
			for _, entry := range entries {
				if entry.Key == db.HeraMetaKeyHandoffNote {
					note = entry.Value
				}
			}
		}
		return r.residentRecycler.restartResident(taskID, note)
	}
	meta, err := r.database.ListMeta(taskID, db.HeraMetaNamespace)
	if err != nil {
		return err
	}
	for _, kv := range meta {
		if kv.Key == db.HeraMetaKeyGraduationPendingRestart && kv.Value != "" {
			return fmt.Errorf("graduation pending restart; recycle deferred")
		}
	}

	binding, err := r.database.HeraLiveBindingByRole(roleID)
	if err != nil {
		return fmt.Errorf("recycle restart: resolve binding for role %d: %w", roleID, err)
	}

	seedPrompt, err := hera.BuildRecycleSeedPrompt(r.database, binding.RoleID, recycleExchangeSection(task, r.cfgFn()))
	if err != nil {
		return fmt.Errorf("recycle restart: build seed prompt for role %d: %w", binding.RoleID, err)
	}

	var kind db.HeraRoleKind
	if role, rerr := r.database.HeraRole(binding.RoleID); rerr == nil && role != nil {
		kind = role.Kind
	}
	return restartSessionWithSeedPrompt(r.database, r.runner, r.cfgFn, task, seedPrompt, kind)
}

// resolveSpawnSize picks the PTY size a restarted session starts at: the fresh
// viewer-reported pane size for the task's role (db.InitialPTYSize — the same
// source agent.CreateAndStart uses at creation), else the outgoing session's
// live PTY size when non-zero, else Start's 24x80 default. source names which
// won ("report" | "pty" | "default") for the log. sess may be nil.
func resolveSpawnSize(database *db.DB, role db.HeraRoleKind, sess interface{ PTYSize() (int, int) }, now time.Time) (rows, cols uint16, source string) {
	if database != nil {
		if size, err := database.InitialPTYSize(role, now); err == nil && size.Valid() {
			return size.Rows, size.Cols, "report"
		}
	}
	if sess != nil {
		if c, r := sess.PTYSize(); c > 0 && r > 0 {
			return uint16(r), uint16(c), "pty" //nolint:gosec // bounded by terminal cell count
		}
	}
	return 24, 80, "default"
}

// restartSessionWithSeedPrompt replaces task.SessionID with a fresh pinned ID (or clears it) and sets task.Prompt to
// seedPrompt, then hands off to the runner's kill/restart-with-empty-context
// primitive (Recycle) — or starts a fresh session directly when no live
// session exists to kill. Shared by HeraRecycleRunner.Restart (hera
// role/binding already resolved into seedPrompt above) and
// taskRecycleRunner.restart (plain task, no hera role — see task_recycle.go).
func restartSessionWithSeedPrompt(database *db.DB, runner agent.SessionRunner, cfgFn func() config.Config, task *model.Task, seedPrompt string, role db.HeraRoleKind) error {
	// The fresh session must open at the pane's reported size, not blindly at the
	// outgoing PTY's: with no automatic size-drift kick
	// (resize-without-restart) nothing repairs a wrong spawn size afterwards.
	// Resolve BEFORE Recycle stops the outgoing session so its live size is
	// still readable as the fallback.
	sess := runner.Get(task.ID)
	var sizer interface{ PTYSize() (int, int) }
	if sess != nil {
		sizer = sess
	}
	rows, cols, source := resolveSpawnSize(database, role, sizer, time.Now())
	slog.Info("[spawn-size] restart", "task", task.ID, "role", role, "source", source, "rows", rows, "cols", cols)

	// task.SessionID MUST be replaced before Recycle: BuildCmd's non-resume
	// branch pins any non-empty SessionID via --session-id, and the OLD
	// (already-used) UUID would collide. BuildCmd never mints one itself, so
	// pin-style families (Claude) get a fresh UUID here — clearing alone left
	// the recycled session with no resumable ID until its next exit capture
	// (ctrl+n re-render refused). Capture-style families get "".
	task.SessionID = agent.FreshSessionID(task, cfgFn())
	if cap, ok := runner.(agent.RecycleIDCapable); ok && !cap.RecycleCarriesSessionID() {
		// A pre-v12 supervisor ignores RecycleReq.SessionID and would launch
		// with no --session-id; persisting the minted ID would leave a phantom
		// ID no transcript matches. Keep the legacy empty ID instead.
		slog.Warn("[recycle] supervisor too old to carry session id; leaving it empty", "task", task.ID)
		task.SessionID = ""
	}
	task.Prompt = seedPrompt
	if err := database.Update(task); err != nil {
		return fmt.Errorf("restart session: persist fresh session id for task %s: %w", task.ID, err)
	}

	if sess == nil {
		// No live session to stop (already exited between the recycle request
		// and this restart). IsIdle already treats this as "proceed" — but
		// Runner.Recycle requires an existing session to stop and would return
		// ErrSessionNotFound here, which would leave a pending recycle intent
		// stuck retrying forever. Start a fresh session directly instead;
		// 80x24 mirrors Start's own new-session default.
		_, startErr := runner.Start(task, cfgFn(), rows, cols, false)
		if startErr != nil {
			return startErr
		}
	} else if err := runner.Recycle(task, cfgFn(), rows, cols); err != nil {
		return err
	}
	// The resident's lifecycle belongs to its shared manager; ordinary workflow
	// status writes are deliberately refused by the DB for resident tasks.
	if task.IsResident() {
		return nil
	}
	uxlog.Log("[recycle] restarted task=%s; restoring in_progress", task.ID)
	slog.Info("[recycle] restarted; restoring in_progress", "task", task.ID)
	return database.SetStatus(task.ID, model.StatusInProgress)
}
