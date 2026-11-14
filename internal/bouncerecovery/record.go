package bouncerecovery

import (
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/transcriptstate"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// classifyFn reads a task's state at death. noTranscript is true for a
// Claude-family task whose transcript is gone: resuming it would fresh-launch
// with the ORIGINAL prompt (a coordinator would re-run its orchestration), so
// such a task is never resumed. A seam for tests.
type classifyFn func(task *model.Task, database *db.DB) (st transcriptstate.State, noTranscript bool)

func classifyTask(task *model.Task, database *db.DB) (transcriptstate.State, bool) {
	cfg := database.Config()
	path, err := agent.ClaudeTranscriptPath(task, cfg)
	if err != nil {
		// Uncertainty is not proof of absence: resume's own launch validation decides.
		slog.Warn("[bounce-recovery] transcript lookup failed", "task", task.ID, "err", err)
		return transcriptstate.State{Kind: transcriptstate.Unknown}, false
	}
	if path == "" {
		return transcriptstate.State{Kind: transcriptstate.Unknown}, agent.HasClaudeTranscript(task, cfg)
	}
	st, err := transcriptstate.FromFile(path)
	if err != nil {
		slog.Warn("[bounce-recovery] classify failed", "task", task.ID, "err", err)
	}
	return st, false
}

// Record writes a ledger entry for each true orphan: the daemon's
// supervisor-restart reconcile calls it after the resume-time session-ID
// refresh. alive reports whether the supervisor still has a live session for a
// task. It returns the number of entries written. Per-task failures are logged
// and skipped; nothing here can block the reconcile.
func Record(database *db.DB, orphanIDs []string, alive func(string) bool, now time.Time) int {
	return record(database, orphanIDs, alive, now, classifyTask)
}

func record(database *db.DB, orphanIDs []string, alive func(string) bool, now time.Time, classify classifyFn) int {
	if alive == nil {
		alive = func(string) bool { return false }
	}
	inSet := make(map[string]bool, len(orphanIDs))
	for _, id := range orphanIDs {
		inSet[id] = true
	}
	bounceID := strconv.FormatInt(now.Unix(), 10)
	written := 0
	recorded := map[string]*Entry{}
	for _, id := range orphanIDs {
		t, err := database.Get(id)
		if err != nil || !recordableTask(database, t) {
			slog.Info("[bounce-recovery] skip task (missing or closed)", "task", id)
			continue
		}
		rd := roleInfo(database, id)
		rd.depth = orchDepth(database, rd.orchID)
		st, noTranscript := classify(t, database)
		e := Entry{
			TaskID: id, KilledAt: now, State: st.Kind, Question: st.Question, Options: st.Options,
			Role: rd.role, RoleName: rd.roleName, TaskName: t.Name, OrchID: rd.orchID,
			CoordTask: rd.coordTask, Depth: rd.depth, BounceID: bounceID,
		}
		e.Status = initialStatus(e, inSet)
		switch {
		case noTranscript:
			e.Status, e.Reason = StatusFailed, "no_transcript"
			slog.Warn("[bounce-recovery] not resuming: Claude transcript missing (a fresh launch would re-run the original prompt)", "task", id, "name", t.Name, "role", rd.role)
			uxlog.Log("[bounce-recovery] not resuming %s (%s): Claude transcript missing", t.Name, id)
		case e.Role != RoleCoordinator && coordinatorClosed(database, e.CoordTask):
			e.Status, e.Reason = StatusSuperseded, "coordinator_closed"
			slog.Info("[bounce-recovery] not resuming: its coordinator was archived or completed", "task", id, "coord", e.CoordTask)
		}
		if err := Save(database, e); err != nil {
			slog.Warn("[bounce-recovery] ledger write failed", "task", id, "err", err)
			continue
		}
		written++
		recorded[id] = &e
		slog.Info("[bounce-recovery] recorded", "task", id, "name", t.Name, "role", rd.role, "state", st.Kind, "status", e.Status, "reason", e.Reason, "depth", rd.depth)
	}
	written += recordIdleTeam(database, recorded, inSet, alive, now, bounceID, classify)
	return written
}

// recordIdleTeam records, for every coordinator recorded this bounce, the
// members of its team that the bounce also killed but that were NOT in_progress:
// idle workers are routinely rolled to in_review, so the orphan sweep never sees
// them. They are listed to their coordinator (so it never believes they are
// alive) but never auto-resumed (status dormant).
func recordIdleTeam(database *db.DB, recorded map[string]*Entry, inSet map[string]bool, alive func(string) bool, now time.Time, bounceID string, classify classifyFn) int {
	var work []*Entry
	seen := map[string]bool{}
	for _, e := range recorded {
		if e.Role == RoleCoordinator {
			work = append(work, e)
			seen[e.TaskID] = true
		}
	}
	written := 0
	for len(work) > 0 {
		c := work[0]
		work = work[1:]
		roles, err := database.ListHeraRoles(c.OrchID, false)
		if err != nil {
			slog.Warn("[bounce-recovery] list team failed", "coord", c.TaskID, "orch", c.OrchID, "err", err)
			continue
		}
		for _, r := range roles {
			if r.Kind == db.HeraKindCoordinator {
				continue
			}
			b, err := database.HeraLiveBindingByRole(r.ID)
			if err != nil || b == nil || b.ArgusTaskID == c.TaskID || recorded[b.ArgusTaskID] != nil || inSet[b.ArgusTaskID] || alive(b.ArgusTaskID) {
				continue
			}
			t, err := database.Get(b.ArgusTaskID)
			if err != nil || !recordableTask(database, t) || t.Status != model.StatusInReview {
				continue
			}
			rd := roleInfo(database, t.ID)
			rd.depth = orchDepth(database, rd.orchID)
			st, _ := classify(t, database)
			e := Entry{
				TaskID: t.ID, KilledAt: now, State: st.Kind, Question: st.Question, Options: st.Options,
				Role: rd.role, RoleName: r.Name, TaskName: t.Name, OrchID: rd.orchID,
				CoordTask: c.TaskID, Depth: rd.depth, BounceID: bounceID,
				Status: StatusDormant, Reason: "in_review",
			}
			if err := Save(database, e); err != nil {
				slog.Warn("[bounce-recovery] ledger write failed", "task", t.ID, "err", err)
				continue
			}
			written++
			recorded[t.ID] = &e
			slog.Info("[bounce-recovery] recorded idle team member (in_review)", "task", t.ID, "name", t.Name, "coord", c.TaskID, "role", rd.role)
			if e.Role == RoleCoordinator && !seen[e.TaskID] {
				seen[e.TaskID] = true
				work = append(work, &e)
			}
		}
	}
	return written
}

func recordableTask(database *db.DB, t *model.Task) bool {
	if t == nil || t.Archived || t.IsResident() || t.Status == model.StatusComplete {
		return false
	}
	meta, err := database.ListMeta(t.ID, db.HeraMetaNamespace)
	if err != nil {
		slog.Warn("[bounce-recovery] close-out lookup failed; skipping record", "task", t.ID, "err", err)
		return false
	}
	for _, kv := range meta {
		if kv.Key == db.HeraMetaKeyReadyToClose && kv.Value == "true" {
			slog.Info("[bounce-recovery] skip closing task in ledger", "task", t.ID)
			return false
		}
	}
	return true
}

// coordinatorClosed reports that a worker's supervising coordinator task was
// archived or completed: the user abandoned that orchestration.
func coordinatorClosed(database *db.DB, coordTask string) bool {
	if coordTask == "" {
		return false
	}
	t, err := database.Get(coordTask)
	return err == nil && t != nil && (t.Archived || t.Status == model.StatusComplete)
}

// initialStatus applies the resume policy (design D2). inSet holds the IDs
// orphaned by this bounce: a worker or freelance whose coordinator is among
// them is delegated to it. Coordinators, including sub-coordinators, are always
// resumed directly (a parent's hera_revive of a live session is a no-op).
func initialStatus(e Entry, inSet map[string]bool) Status {
	if e.Role == RoleCoordinator {
		return StatusPending
	}
	if e.State == transcriptstate.Idle {
		return StatusDormant
	}
	if e.CoordTask != "" && inSet[e.CoordTask] {
		return StatusDelegated
	}
	return unsupervisedStatus(e)
}

// unsupervisedStatus: interrupted agents resume, idle ones stay dormant.
func unsupervisedStatus(e Entry) Status {
	if e.State == transcriptstate.Idle {
		return StatusDormant
	}
	return StatusPending
}

// roleDetails is a task's hera role class, orchestrator, supervising
// coordinator task and orchestrator depth.
type roleDetails struct {
	role      string
	roleName  string
	orchID    int64
	coordTask string
	depth     int
}

// hat is one live binding paired with its role.
type hat struct {
	binding *db.HeraBinding
	role    *db.HeraRole
}

// roleInfo resolves a task's hera role details. Any lookup error degrades to a
// plain task rather than failing the record.
func roleInfo(d *db.DB, taskID string) roleDetails {
	out := roleDetails{role: RolePlain}
	bindings, err := d.ListHeraLiveBindingsByTask(taskID)
	if err != nil {
		return out
	}
	var coordHat, workerHat *hat
	for _, b := range bindings {
		r, err := d.HeraRole(b.RoleID)
		if err != nil || r == nil {
			continue
		}
		h := &hat{binding: b, role: r}
		if r.Kind == db.HeraKindCoordinator {
			if coordHat == nil {
				coordHat = h
			}
		} else if workerHat == nil {
			workerHat = h
		}
	}
	switch {
	case coordHat != nil:
		out.role, out.roleName, out.orchID = RoleCoordinator, coordHat.role.Name, coordHat.binding.OrchestratorID
	case workerHat != nil:
		out.role, out.roleName, out.orchID = RoleWorker, workerHat.role.Name, workerHat.binding.OrchestratorID
		if workerHat.role.Kind == db.HeraKindFreelance {
			out.role = RoleFreelance
		}
	default:
		return out
	}
	// The supervising coordinator comes from the WORKER hat (a sub-coordinator
	// is a worker in its parent's orchestrator too).
	if workerHat != nil {
		out.coordTask = coordinatorTaskOf(d, workerHat.binding.OrchestratorID, taskID)
	}
	return out
}

// coordinatorTaskOf returns the task bound to the coordinator role of orchID.
func coordinatorTaskOf(d *db.DB, orchID int64, selfTask string) string {
	roles, err := d.ListHeraRolesByKind(orchID, db.HeraKindCoordinator)
	if err != nil {
		return ""
	}
	for _, r := range roles {
		b, err := d.HeraLiveBindingByRole(r.ID)
		if err != nil || b == nil || b.ArgusTaskID == selfTask {
			continue
		}
		return b.ArgusTaskID
	}
	return ""
}

// maxOrchDepth bounds the parent-link walk (cycle guard).
const maxOrchDepth = 16

// orchDepth counts parent links above orchID.
func orchDepth(d *db.DB, orchID int64) int {
	cur := orchID
	depth := 0
	for depth < maxOrchDepth {
		parent, _, err := d.HeraOrchLink(cur)
		if err != nil {
			if !errors.Is(err, db.ErrHeraNotFound) {
				slog.Warn("[bounce-recovery] orch link lookup failed", "orch", cur, "err", err)
			}
			break
		}
		depth++
		cur = parent
	}
	return depth
}
