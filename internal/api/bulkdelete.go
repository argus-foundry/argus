package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const maxBulkDeleteTasks = 100

// Job and per-task states.
const (
	deleteJobQueued   = "queued"
	deleteJobRunning  = "running"
	deleteJobComplete = "complete"

	deleteTaskPending   = "pending"
	deleteTaskRunning   = "running"
	deleteTaskSucceeded = "succeeded"
	deleteTaskSkipped   = "skipped"
	deleteTaskFailed    = "failed"

	cleanupPending = "pending"
)

type bulkDeletePreconditions struct {
	AllowDirty      bool `json:"allow_dirty"`
	RequirePRClosed bool `json:"require_pr_closed"`
}

type bulkDeleteReq struct {
	TaskIDs        []string                `json:"task_ids"`
	IdempotencyKey string                  `json:"idempotency_key"`
	Preconditions  bulkDeletePreconditions `json:"preconditions"`
}

type deleteResultView struct {
	TaskID        string  `json:"task_id"`
	State         string  `json:"state"`
	RecordDeleted bool    `json:"record_deleted"`
	Worktree      string  `json:"worktree"`
	LocalBranch   string  `json:"local_branch"`
	RemoteBranch  string  `json:"remote_branch"`
	Error         *string `json:"error"`
}

// deleteSnap records what a task owned before its row was removed, so a
// restarted daemon can finish cleanup without the row.
type deleteSnap struct {
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
	RepoDir  string `json:"repo_dir"`
}

type storedDeleteResult struct {
	deleteResultView
	Snap *deleteSnap `json:"snap,omitempty"`
}

type storedDeleteJob struct {
	Preconditions bulkDeletePreconditions `json:"preconditions"`
	Results       []storedDeleteResult    `json:"results"`
}

// deleteOps are the git-facing operations the job runner needs; tests fake them.
type deleteOps interface {
	IsDirty(worktree string) (bool, error)
	Cleanup(worktree, branch, repoDir string) agent.GitCleanupResult
}

type gitDeleteOps struct{ ctx context.Context }

func (o gitDeleteOps) IsDirty(worktree string) (bool, error) {
	out, _, err := agent.RunTaskGit(o.ctx, worktree, "status", "--porcelain")
	return out != "", err
}

func (o gitDeleteOps) Cleanup(worktree, branch, repoDir string) agent.GitCleanupResult {
	return agent.CleanupTaskGitContext(o.ctx, worktree, branch, repoDir)
}

func (s *Server) deleteOpsOrDefault() deleteOps {
	if s.deleteOps != nil {
		return s.deleteOps
	}
	return gitDeleteOps{ctx: s.deleteContext()}
}

func requestHash(req bulkDeleteReq) string {
	b, _ := json.Marshal(struct {
		IDs []string                `json:"i"`
		Pre bulkDeletePreconditions `json:"p"`
	}{req.TaskIDs, req.Preconditions})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newDeleteJobID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "tdj_" + hex.EncodeToString(b[:])
}

func deleteErrorPtr(s string) *string { return &s }

func (s *Server) handleBulkDeleteTasks(w http.ResponseWriter, r *http.Request) {
	if s.deleteContext().Err() != nil {
		writeErr(w, http.StatusServiceUnavailable, "server_shutting_down", nil)
		return
	}
	var req bulkDeleteReq
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", nil)
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_body", nil)
		return
	}
	if len(req.TaskIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "task_ids is required", nil)
		return
	}
	if len(req.TaskIDs) > maxBulkDeleteTasks {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("at most %d task_ids", maxBulkDeleteTasks), nil)
		return
	}
	seen := make(map[string]bool, len(req.TaskIDs))
	for _, id := range req.TaskIDs {
		if strings.TrimSpace(id) == "" {
			writeErr(w, http.StatusBadRequest, "task_ids must not contain blank ids", nil)
			return
		}
		if seen[id] {
			writeErr(w, http.StatusBadRequest, "task_ids must be unique", nil)
			return
		}
		seen[id] = true
		// The resident is not a task and is not lifecycle-managed; refuse the
		// whole batch like the single-task DELETE does (residentGate cannot see
		// ids carried in the body).
		if s.db != nil {
			if t, err := s.db.Get(id); err == nil && t.IsResident() {
				uxlog.Log("[resident] rejected REST bulk-delete: batch includes the resident")
				writeErr(w, http.StatusConflict, "cannot delete the resident agent: it is not a task and is not lifecycle-managed", nil)
				return
			}
		}
	}

	doc := storedDeleteJob{Preconditions: req.Preconditions}
	for _, id := range req.TaskIDs {
		doc.Results = append(doc.Results, storedDeleteResult{deleteResultView: deleteResultView{
			TaskID: id, State: deleteTaskPending,
			Worktree: cleanupPending, LocalBranch: cleanupPending, RemoteBranch: cleanupPending,
		}})
	}
	raw, _ := json.Marshal(doc)
	job, created, err := s.db.CreateTaskDeleteJob(db.TaskDeleteJob{
		ID: newDeleteJobID(), IdempotencyKey: req.IdempotencyKey, RequestHash: requestHash(req),
		Status: deleteJobQueued, Results: string(raw),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	if !created && job.RequestHash != requestHash(req) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "idempotency_conflict"})
		return
	}
	if created {
		uxlog.Log("[api] bulk-delete: accepted job=%s tasks=%d", job.ID, len(req.TaskIDs))
		s.startDeleteJob(job.ID)
	} else {
		uxlog.Log("[api] bulk-delete: idempotent replay job=%s", job.ID)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID, "status": job.Status})
}

func (s *Server) handleGetTaskDeleteJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.db.GetTaskDeleteJob(r.PathValue("job_id"))
	if err != nil {
		if errors.Is(err, db.ErrDeleteJobNotFound) {
			writeErr(w, http.StatusNotFound, "job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	var doc storedDeleteJob
	if err := json.Unmarshal([]byte(job.Results), &doc); err != nil {
		writeErr(w, http.StatusInternalServerError, "corrupt job", err)
		return
	}
	results := make([]deleteResultView, len(doc.Results))
	for i, res := range doc.Results {
		results[i] = res.deleteResultView
	}
	completed, total := deleteJobProgress(doc)
	writeJSON(w, http.StatusOK, map[string]any{
		"job_id": job.ID, "status": job.Status, "completed": completed, "total": total, "results": results,
	})
}

func deleteJobProgress(doc storedDeleteJob) (completed, total int) {
	for _, r := range doc.Results {
		if r.State == deleteTaskSucceeded || r.State == deleteTaskSkipped || r.State == deleteTaskFailed {
			completed++
		}
	}
	return completed, len(doc.Results)
}

// startDeleteJob runs the job on a goroutine owned by the daemon, so a
// disconnected request never cancels it. A job already running is ignored.
func (s *Server) startDeleteJob(jobID string) {
	s.deleteJobMu.Lock()
	if s.deleteJobsStopping {
		s.deleteJobMu.Unlock()
		return
	}
	if s.deleteJobsActive == nil {
		s.deleteJobsActive = map[string]bool{}
	}
	if s.deleteJobsActive[jobID] {
		s.deleteJobMu.Unlock()
		return
	}
	s.deleteJobsActive[jobID] = true
	s.deleteJobWG.Add(1)
	s.deleteJobMu.Unlock()
	go func() {
		defer s.deleteJobWG.Done()
		defer func() {
			s.deleteJobMu.Lock()
			delete(s.deleteJobsActive, jobID)
			s.deleteJobMu.Unlock()
		}()
		s.runDeleteJob(jobID)
	}()
}

// resumeDeleteJobs restarts every unfinished job after a daemon restart.
func (s *Server) resumeDeleteJobs() {
	jobs, err := s.db.ListUnfinishedTaskDeleteJobs()
	if err != nil {
		uxlog.Log("[api] bulk-delete: resume list failed: %v", err)
		return
	}
	for _, j := range jobs {
		uxlog.Log("[api] bulk-delete: resuming job=%s", j.ID)
		s.startDeleteJob(j.ID)
	}
}

func (s *Server) saveDeleteJob(jobID, status string, doc storedDeleteJob) error {
	previous, err := s.db.GetTaskDeleteJob(jobID)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(doc)
	if err := s.db.SaveTaskDeleteJob(jobID, status, string(raw)); err != nil {
		uxlog.Log("[api] bulk-delete: save failed job=%s err=%v", jobID, err)
		return err
	}
	completed, total := deleteJobProgress(doc)
	var old storedDeleteJob
	if json.Unmarshal([]byte(previous.Results), &old) == nil {
		before, beforeTotal := deleteJobProgress(old)
		if previous.Status == status && before == completed && beforeTotal == total {
			return nil
		}
	}
	events.Emit(model.EventTypeTaskDeleteJobUpdated, "", map[string]any{
		"job_id": jobID, "status": status, "completed": completed, "total": total,
	})
	return nil
}

func (s *Server) runDeleteJob(jobID string) {
	job, err := s.db.GetTaskDeleteJob(jobID)
	if err != nil {
		uxlog.Log("[api] bulk-delete: load failed job=%s err=%v", jobID, err)
		return
	}
	var doc storedDeleteJob
	if err := json.Unmarshal([]byte(job.Results), &doc); err != nil {
		uxlog.Log("[api] bulk-delete: corrupt job=%s err=%v", jobID, err)
		return
	}
	if err := s.saveDeleteJob(jobID, deleteJobRunning, doc); err != nil {
		return
	}
	ctx := s.deleteContext()
	for i := range doc.Results {
		if ctx.Err() != nil {
			return
		}
		res := &doc.Results[i]
		if res.State == deleteTaskSucceeded || res.State == deleteTaskSkipped || res.State == deleteTaskFailed {
			continue
		}
		if err := s.processDeleteTask(jobID, &doc, res); err != nil {
			if !res.RecordDeleted {
				_ = s.saveDeleteJob(jobID, deleteJobRunning, doc)
			}
			return
		}
		if ctx.Err() != nil {
			if res.State == deleteTaskRunning || res.State == deleteTaskFailed {
				res.State = deleteTaskPending
				res.Error = nil
			}
			_ = s.saveDeleteJob(jobID, deleteJobRunning, doc)
			return
		}
		if res.State == deleteTaskRunning {
			res.State = deleteTaskFailed
			res.Error = deleteErrorPtr("interrupted")
		}
		if err := s.saveDeleteJob(jobID, deleteJobRunning, doc); err != nil {
			return
		}
	}
	if ctx.Err() != nil {
		return
	}
	if err := s.saveDeleteJob(jobID, deleteJobComplete, doc); err != nil {
		return
	}
	uxlog.Log("[api] bulk-delete: complete job=%s", jobID)
}

func skipDeleteTask(res *storedDeleteResult, code string) {
	res.State = deleteTaskSkipped
	res.Error = deleteErrorPtr(code)
}

func (s *Server) processDeleteTask(jobID string, doc *storedDeleteJob, res *storedDeleteResult) (persistErr error) {
	defer func() {
		if rec := recover(); rec != nil {
			uxlog.Log("[api] bulk-delete: panic job=%s task=%s: %v\n%s", jobID, res.TaskID, rec, debug.Stack())
			res.State = deleteTaskFailed
			res.Error = deleteErrorPtr("internal error")
		}
	}()
	unlockTask, err := s.lockTaskMutation(s.deleteContext(), res.TaskID)
	if err != nil {
		res.State = deleteTaskRunning
		return nil
	}
	defer unlockTask()
	ownershipHeld := false

	ops := s.deleteOpsOrDefault()
	id := res.TaskID

	if !res.RecordDeleted {
		task, err := s.db.Get(id)
		switch {
		case errors.Is(err, db.ErrTaskNotFound) && res.Snap != nil:
			res.RecordDeleted = true
		case errors.Is(err, db.ErrTaskNotFound):
			skipDeleteTask(res, "task_not_found")
			return nil
		case err != nil || task == nil:
			res.State = deleteTaskFailed
			res.Error = deleteErrorPtr("task lookup failed")
			return nil
		default:
			if !s.preflightDeleteTask(task, doc.Preconditions, ops, res) {
				return nil
			}
			res.State = deleteTaskRunning
			res.Snap = &deleteSnap{
				Worktree: task.Worktree, Branch: task.Branch,
				RepoDir: agent.ResolveDir(task, s.db.Config()),
			}
			unlock, err := agent.LockTaskGitOwnership(s.deleteContext(), res.Snap.RepoDir)
			if err != nil {
				res.State = deleteTaskFailed
				res.Error = deleteErrorPtr("cleanup_interrupted")
				return nil
			}
			defer unlock()
			ownershipHeld = true
			if _, _, _, ok := s.deleteCleanupTargets(res); !ok {
				return nil
			}

			if err := s.saveDeleteJob(jobID, deleteJobRunning, *doc); err != nil {
				res.State = deleteTaskFailed
				res.Error = deleteErrorPtr("snapshot_save_failed")
				return err
			}
			if s.deleteContext().Err() != nil {
				return nil
			}
			s.removeTaskRecord(task, doc.Preconditions, ops, res)
			if !res.RecordDeleted {
				return nil
			}
			if err := s.saveDeleteJob(jobID, deleteJobRunning, *doc); err != nil {
				return err
			}
		}
	}
	res.State = deleteTaskRunning
	if ownershipHeld {
		s.cleanupDeletedTaskOwned(res, ops)
	} else {
		s.cleanupDeletedTask(res, ops)
	}
	return nil
}

func (s *Server) preflightDeleteTask(task *model.Task, pre bulkDeletePreconditions, ops deleteOps, res *storedDeleteResult) bool {
	coord, err := s.db.TaskHoldsLiveHeraCoordinatorBinding(task.ID)
	if err != nil {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("coordinator check failed")
		return false
	}
	if coord {
		skipDeleteTask(res, "hera_cascade_required")
		return false
	}
	if s.db.GraduationPending(task.ID) {
		uxlog.Log("[resident] bulk-delete skipped task=%s: graduation pending", task.ID)
		skipDeleteTask(res, "graduation_pending")
		return false
	}
	if task.Status == model.StatusPending {
		unlock, acquired, err := agent.TryLockTaskGitOwnership(s.deleteContext(), agent.ResolveDir(task, s.db.Config()))
		if err != nil {
			res.State = deleteTaskFailed
			res.Error = deleteErrorPtr("cleanup_interrupted")
			return false
		}
		if !acquired {
			uxlog.Log("[api] bulk-delete: creation pending task=%s", task.ID)
			skipDeleteTask(res, "task_creation_in_progress")
			return false
		}
		unlock()
	}

	if !pre.AllowDirty && task.Worktree != "" {
		if _, serr := os.Stat(task.Worktree); serr == nil {
			dirty, derr := ops.IsDirty(task.Worktree)
			if derr != nil {
				skipDeleteTask(res, "dirty_check_failed")
				return false
			}
			if dirty {
				skipDeleteTask(res, "dirty_worktree")
				return false
			}
		} else if !os.IsNotExist(serr) {
			uxlog.Log("[api] bulk-delete: preflight worktree stat failed task=%s err=%v", task.ID, serr)
			skipDeleteTask(res, "dirty_check_failed")
			return false
		}
	}
	if pre.RequirePRClosed {
		state := ""
		if entries, merr := s.db.ListMeta(task.ID, "pr"); merr == nil {
			for _, e := range entries {
				if e.Key == "state" {
					state = e.Value
				}
			}
		}
		if state != model.PRMergedClosed.String() {
			skipDeleteTask(res, "pr_not_closed")
			return false
		}
	}
	return true
}

func (s *Server) removeTaskRecord(task *model.Task, pre bulkDeletePreconditions, ops deleteOps, res *storedDeleteResult) {
	id := task.ID
	handle := s.runner.Get(id)
	if s.runner.HasPendingRestart(id) {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("session_restart_pending")
		uxlog.Log("[api] bulk-delete: restart pending task=%s", id)
		return
	}
	if err := s.runner.Stop(id); err != nil && !errors.Is(err, agent.ErrSessionNotFound) && err.Error() != agent.ErrSessionNotFound.Error() {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("session_stop_failed")
		uxlog.Log("[api] bulk-delete: stop failed task=%s err=%v", id, err)
		return
	}
	if err := s.waitDeleteSessionExit(id, handle); err != nil {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("session_exit_unconfirmed")
		uxlog.Log("[api] bulk-delete: exit unconfirmed task=%s err=%v", id, err)
		return
	}

	if s.deleteContext().Err() != nil {
		res.State = deleteTaskRunning
		return
	}
	if !pre.AllowDirty && task.Worktree != "" {
		if _, err := os.Stat(task.Worktree); err == nil {
			dirty, err := ops.IsDirty(task.Worktree)
			if err != nil {
				uxlog.Log("[api] bulk-delete: post-exit dirty check failed task=%s err=%v", id, err)
				skipDeleteTask(res, "dirty_check_failed")
				return
			}
			if dirty {
				uxlog.Log("[api] bulk-delete: post-exit dirty worktree preserved task=%s", id)
				skipDeleteTask(res, "dirty_worktree")
				return
			}
			uxlog.Log("[api] bulk-delete: post-exit worktree clean task=%s", id)
		} else if !os.IsNotExist(err) {
			uxlog.Log("[api] bulk-delete: post-exit worktree stat failed task=%s err=%v", id, err)
			skipDeleteTask(res, "dirty_check_failed")
			return
		}
	}
	os.Remove(agent.SessionLogPath(id)) //nolint:errcheck,gosec // G304: id came from db.Get; SessionLogPath roots at ~/.argus/sessions
	if _, derr := s.db.DeleteArtifactsForTask(id); derr != nil {
		uxlog.Log("[api] bulk-delete: artifact row cleanup failed: id=%s err=%v", id, derr)
	}
	os.RemoveAll(agent.ArtifactsDir(id)) //nolint:errcheck,gosec // G304: id came from db.Get
	if err := s.db.Delete(id); err != nil && !errors.Is(err, db.ErrTaskNotFound) {
		uxlog.Log("[api] bulk-delete: record delete failed: id=%s err=%v", id, err)
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("record delete failed")
		return
	}
	res.RecordDeleted = true
	events.Emit(model.EventTypeTaskDeleted, id, nil)
	s.invalidateColsCache(id)
}

// cleanupDeletedTask runs git cleanup from the persisted snapshot. Paths or
// branches that any surviving task now references are left alone, so a repeat
// after restart cannot remove a newer task's worktree.
func (s *Server) cleanupDeletedTask(res *storedDeleteResult, ops deleteOps) {
	snap := res.Snap
	if snap == nil {
		snap = &deleteSnap{}
	}
	unlock, err := agent.LockTaskGitOwnership(s.deleteContext(), snap.RepoDir)
	if err != nil {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("cleanup_interrupted")
		return
	}
	defer unlock()
	s.cleanupDeletedTaskOwned(res, ops)
}

func (s *Server) cleanupDeletedTaskOwned(res *storedDeleteResult, ops deleteOps) {
	worktree, branch, protected, ok := s.deleteCleanupTargets(res)
	if !ok {
		return
	}
	snap := res.Snap
	if snap == nil {
		snap = &deleteSnap{}
	}

	if s.deleteContext().Err() != nil {
		res.State = deleteTaskRunning
		return
	}

	appeared, err := s.deleteSessionPresent(res.TaskID)
	if err != nil {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("session_check_failed")
		uxlog.Log("[api] bulk-delete: session check failed task=%s err=%v", res.TaskID, err)
		return
	}
	if appeared {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("session_appeared")
		uxlog.Log("[api] bulk-delete: session appeared task=%s", res.TaskID)
		return
	}

	out := ops.Cleanup(worktree, branch, snap.RepoDir)
	res.Worktree, res.LocalBranch, res.RemoteBranch = out.Worktree, out.LocalBranch, out.RemoteBranch
	if out.Failed() {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr(out.Err)
		uxlog.Log("[api] bulk-delete: cleanup failed task=%s worktree=%s local=%s remote=%s", res.TaskID, out.Worktree, out.LocalBranch, out.RemoteBranch)
		return
	}
	res.State = deleteTaskSucceeded
	res.Error = nil
	if protected {
		res.Error = deleteErrorPtr("ownership_protected")
		uxlog.Log("[api] bulk-delete: protected survivor resources task=%s", res.TaskID)
	}
	uxlog.Log("[api] bulk-delete: task=%s deleted worktree=%s local=%s remote=%s", res.TaskID, out.Worktree, out.LocalBranch, out.RemoteBranch)
}

func (s *Server) deleteCleanupTargets(res *storedDeleteResult) (worktree, branch string, protected, ok bool) {
	snap := res.Snap
	if snap == nil {
		snap = &deleteSnap{}
	}

	worktree, branch = snap.Worktree, agent.TaskGitCleanupBranch(snap.Worktree, snap.Branch)
	tasks, err := s.db.TasksChecked()
	if err != nil {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("ownership_check_failed")
		uxlog.Log("[api] bulk-delete: ownership failed task=%s err=%v", res.TaskID, err)
		return "", "", false, false
	}
	protected = false
	cfg := s.db.Config()
	identities := map[string]string{}
	targetRepoID, err := agent.TaskGitRepositoryID(s.deleteContext(), snap.RepoDir)
	if err != nil {
		res.State = deleteTaskFailed
		res.Error = deleteErrorPtr("repository_identity_failed")
		return "", "", false, false
	}
	identities[snap.RepoDir] = targetRepoID
	for _, t := range tasks {
		if t.ID == res.TaskID {
			continue
		}
		if worktree != "" && agent.SamePath(t.Worktree, worktree) {
			worktree, branch = "", ""
			protected = true
		}
		if branch == "" || (branch != t.Branch && branch != strings.TrimPrefix(t.BaseBranch, "origin/") && "refs/heads/"+branch != t.BaseBranch && "refs/remotes/origin/"+branch != t.BaseBranch) {
			continue
		}
		repo := agent.ResolveDir(t, cfg)
		repoID, ok := identities[repo]
		if !ok {
			repoID, err = agent.TaskGitRepositoryID(s.deleteContext(), repo)
			if err != nil {
				res.State = deleteTaskFailed
				res.Error = deleteErrorPtr("repository_identity_failed")
				uxlog.Log("[api] bulk-delete: repository identity failed task=%s err=%v", res.TaskID, err)
				return "", "", false, false
			}
			identities[repo] = repoID
		}
		if repo == "" || agent.SamePath(repoID, targetRepoID) {
			branch = ""
			protected = true
		}
	}
	return worktree, branch, protected, true
}
