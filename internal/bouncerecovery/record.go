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
)

// classifyFn reads a task's state at death; a seam for tests.
type classifyFn func(task *model.Task, database *db.DB) transcriptstate.State

func classifyTask(task *model.Task, database *db.DB) transcriptstate.State {
	path, err := agent.ClaudeTranscriptPath(task, database.Config())
	if err != nil || path == "" {
		slog.Info("[bounce-recovery] no transcript to classify", "task", task.ID, "err", err)
		return transcriptstate.State{Kind: transcriptstate.Unknown}
	}
	st, err := transcriptstate.FromFile(path)
	if err != nil {
		slog.Warn("[bounce-recovery] classify failed", "task", task.ID, "err", err)
	}
	return st
}

// Record writes a ledger entry for each true orphan: the daemon's
// supervisor-restart reconcile calls it after the resume-time session-ID
// refresh. It returns the number of entries written. Per-task failures are
// logged and skipped; nothing here can block the reconcile.
func Record(database *db.DB, orphanIDs []string, now time.Time) int {
	return record(database, orphanIDs, now, classifyTask)
}

func record(database *db.DB, orphanIDs []string, now time.Time, classify classifyFn) int {
	inSet := make(map[string]bool, len(orphanIDs))
	for _, id := range orphanIDs {
		inSet[id] = true
	}
	bounceID := strconv.FormatInt(now.Unix(), 10)
	written := 0
	for _, id := range orphanIDs {
		t, err := database.Get(id)
		if err != nil || t == nil || t.Archived || t.IsResident() {
			slog.Info("[bounce-recovery] skip task (missing, archived or resident)", "task", id)
			continue
		}
		role, roleName, orchID, coordTask, depth := roleInfo(database, id)
		st := classify(t, database)
		e := Entry{
			TaskID: id, KilledAt: now, State: st.Kind, Question: st.Question, Options: st.Options,
			Role: role, RoleName: roleName, TaskName: t.Name, OrchID: orchID,
			CoordTask: coordTask, Depth: depth, BounceID: bounceID,
		}
		e.Status = initialStatus(e, inSet)
		if err := Save(database, e); err != nil {
			slog.Warn("[bounce-recovery] ledger write failed", "task", id, "err", err)
			continue
		}
		written++
		slog.Info("[bounce-recovery] recorded", "task", id, "name", t.Name, "role", role, "state", st.Kind, "status", e.Status, "depth", depth)
	}
	return written
}

// initialStatus applies the resume policy (design D2). inSet holds the IDs
// orphaned by this bounce: a worker whose coordinator is among them is
// delegated to it.
func initialStatus(e Entry, inSet map[string]bool) Status {
	if e.Role == RoleCoordinator {
		return StatusPending
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

// roleInfo resolves a task's hera role class, orchestrator, supervising
// coordinator task and orchestrator depth. Any lookup error degrades to a
// plain task rather than failing the record.
func roleInfo(d *db.DB, taskID string) (role, roleName string, orchID int64, coordTask string, depth int) {
	role = RolePlain
	bindings, err := d.ListHeraLiveBindingsByTask(taskID)
	if err != nil || len(bindings) == 0 {
		return
	}
	var workerB, coordB *db.HeraBinding
	var workerRole, coordRole *db.HeraRole
	for _, b := range bindings {
		r, err := d.HeraRole(b.RoleID)
		if err != nil || r == nil {
			continue
		}
		if r.Kind == db.HeraKindCoordinator {
			if coordB == nil {
				coordB, coordRole = b, r
			}
		} else if workerB == nil {
			workerB, workerRole = b, r
		}
	}
	switch {
	case coordB != nil:
		role, roleName, orchID = RoleCoordinator, coordRole.Name, coordB.OrchestratorID
	case workerB != nil:
		role, roleName, orchID = RoleWorker, workerRole.Name, workerB.OrchestratorID
		if workerRole.Kind == db.HeraKindFreelance {
			role = RoleFreelance
		}
	default:
		return
	}
	// The supervising coordinator comes from the WORKER hat (a sub-coordinator
	// is a worker in its parent's orchestrator too).
	if workerB != nil {
		coordTask = coordinatorTaskOf(d, workerB.OrchestratorID, taskID)
	}
	depth = orchDepth(d, orchID)
	return
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

// orchDepth counts parent links above orchID (cycle-guarded).
func orchDepth(d *db.DB, orchID int64) int {
	depth := 0
	for i := 0; i < 16; i++ {
		parent, _, err := d.HeraOrchLink(orchID)
		if err != nil {
			if !errors.Is(err, db.ErrHeraNotFound) {
				slog.Warn("[bounce-recovery] orch link lookup failed", "orch", orchID, "err", err)
			}
			return depth
		}
		depth++
		orchID = parent
	}
	return depth
}
