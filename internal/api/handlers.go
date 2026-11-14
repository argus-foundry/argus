package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/clipboard"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/links"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/profiles"
	"github.com/argus-foundry/argus/internal/sanitize"
	"github.com/argus-foundry/argus/internal/skills"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// --- Status ---

type statusResponse struct {
	OK       bool          `json:"ok"`
	Sessions sessionCounts `json:"sessions"`
	Tasks    taskCounts    `json:"tasks"`
}

type sessionCounts struct {
	Running int `json:"running"`
	Idle    int `json:"idle"`
}

type taskCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	InReview   int `json:"in_review"`
	Complete   int `json:"complete"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	running, idle := s.runner.RunningAndIdle()
	idle = bridge.Override(idle, s.BridgeStates(), bridge.Quiescent)

	tasks, err := s.db.Tasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tasks", err)
		return
	}
	var tc taskCounts
	for _, t := range tasks {
		if t.Archived {
			continue
		}
		switch t.Status {
		case model.StatusPending:
			tc.Pending++
		case model.StatusInProgress:
			tc.InProgress++
		case model.StatusInReview:
			tc.InReview++
		case model.StatusComplete:
			tc.Complete++
		}
	}

	writeJSON(w, http.StatusOK, statusResponse{
		OK:       true,
		Sessions: sessionCounts{Running: len(running), Idle: len(idle)},
		Tasks:    tc,
	})
}

// --- List Tasks ---

// taskJSON is the wire shape returned by /api/tasks*.
//
// Status is the persisted DB status. Idle is a runtime-derived flag that is
// true only when a live session is waiting for input; an absent session is not
// idle. omitempty drops idle:false from the JSON; the SPA reads missing fields
// as falsy, which matches the intended contract (no idle field == not idle).
type taskJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Idle   bool   `json:"idle,omitempty"`
	// Live is true when the runner holds a real session for the task,
	// including one parked idle at a prompt, and no kill+resume restart is
	// pending, whatever the workflow status. Deliberately NOT omitempty: an
	// absent field means an older daemon, and clients fall back to !idle.
	Live bool `json:"live"`
	// NeedsInput is a runtime-derived flag, true only when Status ==
	// in_progress and the daemon's watcher detected the agent is blocked
	// waiting on the user (the red ? in the TUI). omitempty drops the field
	// when false, matching the Idle contract (missing == not blocked).
	NeedsInput   bool   `json:"needs_input,omitempty"`
	Project      string `json:"project"`
	Branch       string `json:"branch,omitempty"`
	Backend      string `json:"backend,omitempty"`
	Elapsed      string `json:"elapsed,omitempty"`
	CreatedAt    string `json:"created_at"`
	Archived     bool   `json:"archived,omitempty"`
	Pinned       bool   `json:"pinned"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	// PRState is the cached GitHub PR review state for the task's branch,
	// sourced from task_meta namespace "pr" (key "state") written by the
	// daemon poller. The handler NEVER shells out to gh — this is a pure
	// cache read. omitempty drops "none"/empty so the SPA only sees
	// actionable values; the badge renderer ignores draft/merged-closed/
	// unknown anyway.
	PRState string `json:"pr_state,omitempty"`
	// PRLifecycle is open/merged/closed/unknown from the cached GitHub result;
	// unknown until verified. Not omitempty so clients can tell an old daemon.
	PRLifecycle string `json:"pr_lifecycle"`
	// Failure is the explicit agent-reported failure parsed from Result, or
	// null. Not omitempty so clients can tell "no failure" from an old daemon.
	Failure *model.TaskFailure `json:"failure"`
}

// taskRuntimeState carries non-persisted, derived-at-request-time fields used
// to populate taskJSON. Group runtime flags here so the taskToJSON signature
// doesn't grow into a positional bool chain when more flags are added.
type taskRuntimeState struct {
	Idle       bool
	Live       bool
	NeedsInput bool
	// PRState is the cached PR review state string ("awaiting-review", etc.)
	// from task_meta namespace "pr". Empty / "none" maps to an omitted DTO
	// field via omitempty. Populated by the caller from a batch task_meta
	// read — never by shelling out to gh.
	PRState string
	// PRLifecycle is the cached lifecycle string; resolved via
	// model.ResolvePRLifecycle with PRState.
	PRLifecycle string
	// Failure is the projection of the task's Result; nil when not failed.
	Failure *model.TaskFailure
}

func taskToJSON(t *model.Task, rt taskRuntimeState) taskJSON {
	return taskJSON{
		ID:           t.ID,
		Name:         t.Name,
		Status:       t.Status.String(),
		Idle:         rt.Idle,
		Live:         rt.Live,
		NeedsInput:   rt.NeedsInput,
		Project:      t.Project,
		Branch:       t.Branch,
		Backend:      t.Backend,
		Elapsed:      t.ElapsedString(),
		CreatedAt:    t.CreatedAt.Format(time.RFC3339),
		Archived:     t.Archived,
		Pinned:       t.Pinned,
		WorktreePath: t.Worktree,
		Prompt:       t.Prompt,
		PRState:      prDTOValue(rt.PRState),
		PRLifecycle:  string(model.ResolvePRLifecycle(rt.PRState, rt.PRLifecycle)),
		Failure:      rt.Failure,
	}
}

// prDTOValue normalizes a cached PR state string for the DTO. "none" (and the
// empty string) collapse to "" so omitempty drops the field; every other value
// passes through verbatim. Centralizing the rule keeps handleListTasks and
// handleGetTask consistent.
func prDTOValue(state string) string {
	if state == "" || state == model.PRNone.String() {
		return ""
	}
	return state
}

// computeRuntimeState derives the per-request runtime state for a task. Live
// is session existence and is independent of workflow status; an absent
// session is never idle. NeedsInput keeps its in_progress gate and requires a
// live session.
func computeRuntimeState(t *model.Task, runningSet, idleSet, needsInputSet map[string]bool) taskRuntimeState {
	live := runningSet[t.ID]
	return taskRuntimeState{
		Idle:       live && idleSet[t.ID],
		Live:       live,
		NeedsInput: live && t.Status == model.StatusInProgress && needsInputSet[t.ID],
	}
}

// runtimeState wraps computeRuntimeState with the one signal the pure maps
// can't carry: RunningAndIdle lists a pending-restart task as running even
// though runner.Get is nil for it (stream, input and resize all 404). Such a
// task is mid kill+resume and must not report Live.
func (s *Server) runtimeState(t *model.Task, runningSet, idleSet, needsInputSet map[string]bool) taskRuntimeState {
	rt := computeRuntimeState(t, runningSet, idleSet, needsInputSet)
	if rt.Live && s.runner.HasPendingRestart(t.ID) {
		rt = taskRuntimeState{}
	}
	return rt
}

func (s *Server) sessionStateMaps() (runningSet, idleSet, needsInputSet map[string]bool) {
	running, idle := s.runner.RunningAndIdle()
	runningSet = make(map[string]bool, len(running))
	for _, id := range running {
		runningSet[id] = true
	}
	idleSet = make(map[string]bool, len(idle))
	for _, id := range idle {
		idleSet[id] = true
	}
	needs := s.runner.NeedsInputIDs()
	needsInputSet = make(map[string]bool, len(needs))
	for _, id := range needs {
		needsInputSet[id] = true
	}
	for id, st := range s.BridgeStates() {
		idleSet[id] = bridge.Quiescent(st)
		needsInputSet[id] = st.NeedsInput
	}
	return runningSet, idleSet, needsInputSet
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.db.Tasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tasks", err)
		return
	}

	// Optional filters.
	statusFilter := r.URL.Query().Get("status")
	projectFilter := r.URL.Query().Get("project")
	// archived: "0" (default) excludes archived; "1" returns only archived;
	// "all" returns both.
	archivedFilter := r.URL.Query().Get("archived")

	runningSet, idleSet, needsInputSet := s.sessionStateMaps()

	// Batch-read the cached PR review state once (one indexed query) rather
	// than per-task. The daemon poller is the sole writer; this handler is a
	// pure cache read and MUST NOT shell out to gh. A read failure is
	// non-fatal — we just omit pr_state for this response.
	prMeta, err := s.db.ListMetaByNamespace("pr")
	if err != nil {
		uxlog.Log("[pr] api: list pr meta failed: %v", err)
		prMeta = nil
	}

	result := make([]taskJSON, 0)
	for _, t := range tasks {
		switch archivedFilter {
		case "1":
			if !t.Archived {
				continue
			}
		case "all":
			// no filter
		default:
			if t.Archived {
				continue
			}
		}
		if statusFilter != "" && t.Status.String() != statusFilter {
			continue
		}
		if projectFilter != "" && t.Project != projectFilter {
			continue
		}
		rt := s.runtimeState(t, runningSet, idleSet, needsInputSet)
		if kv := prMeta[t.ID]; kv != nil {
			rt.PRState = kv["state"]
			rt.PRLifecycle = kv["lifecycle"]
		}
		rt.Failure = model.FailureFromResult(t.Result, t.ResultAt)
		result = append(result, taskToJSON(t, rt))
	}

	writeJSON(w, http.StatusOK, map[string]any{"tasks": result})
}

// --- Get Task ---

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	runningSet, idleSet, needsInputSet := s.sessionStateMaps()
	rt := s.runtimeState(task, runningSet, idleSet, needsInputSet)
	rt.Failure = model.FailureFromResult(task.Result, task.ResultAt)
	// Pure cache read of the daemon-written PR state; never shells out to gh.
	if entries, merr := s.db.ListMeta(task.ID, "pr"); merr == nil {
		for _, e := range entries {
			switch e.Key {
			case "state":
				rt.PRState = e.Value
			case "lifecycle":
				rt.PRLifecycle = e.Value
			}
		}
	} else {
		uxlog.Log("[pr] api: list pr meta for %s failed: %v", task.ID, merr)
	}
	writeJSON(w, http.StatusOK, taskToJSON(task, rt))
}

// --- Create Task ---

// validateBackend returns nil if name is empty (caller will fall back to the
// configured default) or matches a configured backend. Returns an error if the
// caller asked for a backend that isn't in the config — fail fast at the API
// boundary instead of letting agent.CreateAndStart roll back the worktree
// after the fact.
func (s *Server) validateBackend(name string) error {
	if name == "" || name == config.DynamicBackend {
		return nil
	}
	cfg := s.db.Config()
	if _, ok := cfg.Backends[name]; !ok {
		return fmt.Errorf("backend %q not configured", name)
	}
	return nil
}

// validateAccount rejects an unknown or misconfigured account name, or one
// that cannot run an explicitly requested backend, so a bad selection fails
// the request instead of creating a task that cannot spawn. An implicit
// backend is checked later by agent.CreateAndStart.
func (s *Server) validateAccount(name, backend string) error {
	if name == "" {
		return nil
	}
	cfg := s.db.Config()
	if err := cfg.ValidateAccount(name); err != nil {
		return err
	}
	if b, ok := cfg.Backends[backend]; ok && !cfg.AccountSupports(name, b.Command) {
		return fmt.Errorf("account %q does not support backend %q", name, backend)
	}
	return nil
}

// validateSandboxOverride returns nil for the three accepted tri-state values
// ("" inherit, "enabled", "disabled" — add-task-sandbox-override) and an error
// for anything else, so a malformed value is rejected at the API boundary
// instead of silently resolving as "inherit".
func validateSandboxOverride(v string) error {
	return model.ValidateSandboxOverride(v)
}

type createTaskReq struct {
	Name    string `json:"name"`
	Prompt  string `json:"prompt"`
	Project string `json:"project"`
	// Backend overrides the default backend for this task. Empty = use the
	// per-project / global default. Validated against the configured backends
	// before the task is created.
	Backend string `json:"backend"`
	// Model overrides the backend's default model for this task. Empty = use
	// the backend's configured default (or the CLI's own default). Validated
	// against the resolved backend's model policy before anything is created.
	Model string `json:"model"`
	// SandboxOverride is a tri-state per-task override of the resolved sandbox
	// setting: "" (inherit the project/global setting), "enabled" (force
	// sandboxed), "disabled" (force unsandboxed). Validated against the three
	// accepted values before the task is created (add-task-sandbox-override).
	SandboxOverride string `json:"sandbox_override"`
	// Account selects a configured account for this task. Empty =
	// resolve the project/global default. Validated before the task is created.
	Account string `json:"account"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	// Multipart bodies carry user-uploaded attachments alongside the task
	// fields. They must be handled differently because (a) we need to copy
	// files into the worktree before the agent starts, and (b) the body
	// can be much larger than 1MB.
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		s.handleCreateTaskMultipart(w, r)
		return
	}

	var req createTaskReq
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	if req.Project == "" {
		writeErr(w, http.StatusBadRequest, "project is required", nil)
		return
	}
	if req.Prompt == "" && req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name or prompt is required", nil)
		return
	}
	if err := s.validateBackend(req.Backend); err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	if err := validateSandboxOverride(req.SandboxOverride); err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	if err := s.validateAccount(req.Account, req.Backend); err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	autoName := req.Name == ""
	name := req.Name
	if name == "" {
		// Generate name from prompt (first 40 chars, sanitized).
		name = sanitizeName(req.Prompt)
	}

	task, err := s.createTask(name, req.Prompt, req.Project, req.Backend, req.Model, req.SandboxOverride, req.Account, autoName)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, agent.ErrAccount) {
			status = http.StatusBadRequest
		}
		if writeModelNotAllowed(w, err) {
			return
		}
		writeErr(w, status, "", err)
		return
	}

	// task.Name here is the regex slug. When autoName is true, a Haiku
	// rename fires asynchronously after this response — clients that re-list
	// or stream tasks will see the updated name within a few seconds.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     task.ID,
		"name":   task.Name,
		"status": task.Status.String(),
	})
}

// handleCreateTaskMultipart handles POST /api/tasks with a multipart body —
// task fields (name/prompt/project) plus zero or more file parts. Attachments
// are written to <worktree>/.context/ before the session starts; their paths
// are appended to the prompt so the agent sees them on its first turn.
//
// Bypasses s.createTask (the daemon-injected callback) and calls
// agent.CreateAndStart directly because the callback's narrow signature has
// no place for attachments and the API server runs in the daemon process so
// it has direct access to db/runner.
func (s *Server) handleCreateTaskMultipart(w http.ResponseWriter, r *http.Request) {
	// 50MB total cap + headroom for the multipart envelope and text fields.
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentTotalBytes+1<<20)

	name, prompt, project, backend, taskModel, sandboxOverride, account, atts, err := parseMultipartTaskForm(r)
	if err != nil {
		writeErr(w, statusForUploadErr(err), "", err)
		return
	}
	if project == "" {
		writeErr(w, http.StatusBadRequest, "project is required", nil)
		return
	}
	if prompt == "" && name == "" && len(atts) == 0 {
		writeErr(w, http.StatusBadRequest, "name, prompt, or files required", nil)
		return
	}
	if err := s.validateBackend(backend); err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	if err := validateSandboxOverride(sandboxOverride); err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	if err := s.validateAccount(account, backend); err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	// autoName fires only when name was synthesized from prompt — not from
	// an attachment filename (which is already meaningful) and not when the
	// user typed a name explicitly.
	autoName := name == "" && prompt != ""
	if name == "" {
		if prompt != "" {
			name = sanitizeName(prompt)
		} else {
			name = sanitizeName(atts[0].Name)
		}
	}

	task, _, err := agent.CreateAndStart(s.db, s.runner, agent.CreateInput{
		Name:            name,
		Prompt:          prompt,
		Project:         project,
		Backend:         backend,
		Model:           taskModel,
		SandboxOverride: sandboxOverride,
		Account:         account,
		Attachments:     atts,
		AutoName:        autoName,
	})
	if err != nil {
		uxlog.Log("[uploads] create task failed name=%q project=%q files=%d err=%v", name, project, len(atts), err)
		status := http.StatusInternalServerError
		if errors.Is(err, agent.ErrAccount) {
			status = http.StatusBadRequest
		}
		if writeModelNotAllowed(w, err) {
			return
		}
		writeErr(w, status, "", err)
		return
	}

	uxlog.Log("[uploads] task created id=%s name=%q files=%d", task.ID, task.Name, len(atts))
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     task.ID,
		"name":   task.Name,
		"status": task.Status.String(),
	})
}

// --- Stop Task ---

func (s *Server) handleStopTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}

	if err := s.runner.Stop(id); err != nil && !errors.Is(err, agent.ErrSessionNotFound) {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}

	task.SetStatus(model.StatusInReview)
	s.db.Update(task) //nolint:errcheck

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// --- Restart Task ---

// handleRestartTask re-spawns a finished agent session via
// POST /api/tasks/{id}/restart. It is the argus side of hera's agent-reattach
// feature: hera calls this endpoint when the user presses Enter on a dead
// session row so the prior conversation is resumed in the same worktree.
//
// Behaviour:
//   - 404 if the task does not exist.
//   - 409 if a non-idle session is already running (agent is live — no restart
//     needed).
//   - 200 + {"status":"restarted","pid":<n>} on success. The new PTY session
//     streams through the same ring-buffer / SSE path that consumers (hera's
//     proxy subscription, the SPA terminal) already listen on, so they pick up
//     the resumed output automatically without re-subscribing.
//
// Resume is requested so saved conversations reopen; the shared runner
// recovers a missing Claude transcript with a fresh ID and original prompt.
func (s *Server) handleRestartTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force, ok := parseForceRevive(w, r)
	if !ok {
		return
	}
	unlock, lockErr := s.lockTaskMutation(r.Context(), id)
	if lockErr != nil {
		uxlog.Log("[api] restart: task lock canceled task=%s err=%v", id, lockErr)
		writeErr(w, http.StatusServiceUnavailable, "task operation canceled", nil)
		return
	}
	defer unlock()

	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
		return
	}

	// 409 only when the agent is actively working. Idle in_progress tasks
	// (no live session, or session.IsIdle()) are exactly what this endpoint
	// targets — they should be restarted, not rejected.
	if sess := s.runner.Get(task.ID); sess != nil && !sess.IsIdle() {
		writeCodedErrCause(w, http.StatusConflict, "task already running", "task_running", nil)
		return
	}

	if s.runner.HasPendingRestart(task.ID) {
		uxlog.Log("[api] restart: refused pending restart task=%s", id)
		writeCodedErrCause(w, http.StatusConflict, "agent is restarting", "agent_restarting", nil)
		return
	}

	proceed, rollback := s.closeoutGate(w, task, force, "restart")
	if !proceed {
		return
	}

	reattached, err := s.restartTaskSession(task)
	if err != nil {
		if !errors.Is(err, agent.ErrStartAmbiguous) {
			rollback()
		}
		uxlog.Log("[api] restart: start failed task=%s err=%v", id, err)
		writeLaunchErr(w, err)
		return
	}

	if reattached {
		uxlog.Log("[api] restart: healed task=%s pid=%d", id, task.AgentPID)
	} else {
		uxlog.Log("[api] restart: started task=%s pid=%d resume=%t", id, task.AgentPID, task.SessionID != "")
	}

	resp := map[string]any{
		"status":     "restarted",
		"pid":        task.AgentPID,
		"session_id": task.SessionID,
	}
	if reattached {
		resp["healed"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeLaunchErr(w http.ResponseWriter, err error) {
	if errors.Is(err, agent.ErrStartAmbiguous) {
		writeCodedErrCause(w, http.StatusServiceUnavailable, "session start outcome is unknown; check task runtime before retrying", "start_ambiguous", nil)
		return
	}
	writeCodedErrCause(w, http.StatusInternalServerError, "", "start_failed", err)
}

type reviveReq struct {
	ForceRevive bool `json:"force_revive"`
}

// parseForceRevive reads the optional {"force_revive":bool} body; an empty
// body means false. It writes the 400 itself and reports ok=false on junk.
func parseForceRevive(w http.ResponseWriter, r *http.Request) (force, ok bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil {
		writeCodedErrCause(w, http.StatusBadRequest, "", "invalid_body", err)
		return false, false
	}
	if len(raw) > 1024 {
		writeCodedErrCause(w, http.StatusBadRequest, "body exceeds 1024 bytes", "invalid_body", nil)
		return false, false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, true
	}
	if bytes.TrimSpace(raw)[0] != '{' {
		writeCodedErrCause(w, http.StatusBadRequest, "expected JSON object", "invalid_body", nil)
		return false, false
	}
	var req reviveReq
	if err := json.Unmarshal(raw, &req); err != nil {
		writeCodedErrCause(w, http.StatusBadRequest, "invalid JSON", "invalid_body", err)
		return false, false
	}
	return req.ForceRevive, true
}

// closeoutGate applies the TUI's close-out guard to a REST resume/restart: a
// worker or freelance task awaiting Hera close-out is refused unless the
// caller deliberately forces the revival. On a forced revival it clears the
// close-out state up front and returns a rollback that restores it if the
// launch then fails. Callers hold the per-task lock.
func (s *Server) closeoutGate(w http.ResponseWriter, task *model.Task, force bool, op string) (proceed bool, rollback func()) {
	noop := func() {}
	workerOrFreelance, err := s.db.TaskHoldsLiveHeraWorkerOrFreelanceBinding(task.ID)
	if err != nil {
		uxlog.Log("[api] %s: closeout check failed task=%s err=%v", op, task.ID, err)
		writeCodedErrCause(w, http.StatusInternalServerError, "", "closeout_check_failed", err)
		return false, noop
	}
	if !workerOrFreelance {
		return true, noop
	}
	awaiting, err := s.db.HeraWorkerAwaitingCloseout(task.ID)
	if err != nil {
		uxlog.Log("[api] %s: closeout check failed task=%s err=%v", op, task.ID, err)
		writeCodedErrCause(w, http.StatusInternalServerError, "", "closeout_check_failed", err)
		return false, noop
	}
	if !awaiting {
		return true, noop
	}
	if !force {
		uxlog.Log("[api] %s: refused closed-out task=%s", op, task.ID)
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":            "task awaits Hera close-out",
			"code":             "hera_closeout",
			"can_force_revive": true,
		})
		return false, noop
	}
	snap, err := s.db.SnapshotAndClearHeraCloseout(task.ID)
	if err != nil {
		uxlog.Log("[api] %s: force-revive clear failed task=%s err=%v", op, task.ID, err)
		writeCodedErrCause(w, http.StatusInternalServerError, "", "closeout_clear_failed", err)
		return false, noop
	}
	uxlog.Log("[api] %s: force-reviving closed-out task=%s", op, task.ID)
	return true, func() {
		if rerr := s.db.RestoreHeraCloseout(snap); rerr != nil {
			uxlog.Log("[api] %s: restore closeout failed task=%s err=%v", op, task.ID, rerr)
		}
	}
}

// restartTaskSession resumes a task's session (or reattaches a live one) and
// marks it in progress. Shared by restart and the account move.
func (s *Server) restartTaskSession(task *model.Task) (reattached bool, err error) {
	return s.startTaskSession(task, true)
}

// --- Resume Task ---

func (s *Server) handleResumeTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force, ok := parseForceRevive(w, r)
	if !ok {
		return
	}
	unlock, lockErr := s.lockTaskMutation(r.Context(), id)
	if lockErr != nil {
		uxlog.Log("[api] resume: task lock canceled task=%s err=%v", id, lockErr)
		writeErr(w, http.StatusServiceUnavailable, "task operation canceled", nil)
		return
	}
	defer unlock()

	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
		return
	}

	if sess := s.runner.Get(task.ID); sess != nil && !sess.IsIdle() {
		writeCodedErrCause(w, http.StatusConflict, "task already running", "task_running", nil)
		return
	}
	if s.runner.HasPendingRestart(task.ID) {
		uxlog.Log("[api] resume: refused pending restart task=%s", id)
		writeCodedErrCause(w, http.StatusConflict, "agent is restarting", "agent_restarting", nil)
		return
	}

	proceed, rollback := s.closeoutGate(w, task, force, "resume")
	if !proceed {
		return
	}

	// StartOrReattach handles the desync case where the daemon still owns a
	// live PTY but the DB row drifted off in_progress (manual edit,
	// post-reconcile state). Calling Start directly there would fail with
	// "session already exists for task X". On reattach we re-sync the row
	// to in_progress so the PWA can attach the terminal stream and other
	// observers see consistent state.
	cfg := s.db.Config()
	resume := task.SessionID != ""
	prevStatus := task.Status

	// Resume-time recapture (Claude-only): re-derive the newest worktree
	// transcript so the resume picks the most recent in-place session, not the
	// stale create-time UUID. No-op for non-Claude / no-change / no-transcript,
	// and never populates an empty ID, so the resume decision above is unchanged.
	agent.RefreshResumeSessionID(s.db, task)
	_ = agent.RestoreAcceptedCoordinatorHat(s.db, task.ID) // revived accepted sub-coordinator

	sess, reattached, err := s.runner.StartOrReattach(task, cfg, 24, 80, resume)
	if err != nil {
		if !errors.Is(err, agent.ErrStartAmbiguous) {
			rollback()
		}
		uxlog.Log("[api] resume: start failed task=%s status=%s err=%v", id, prevStatus, err)
		writeLaunchErr(w, err)
		return
	}

	task.SetStatus(model.StatusInProgress)
	task.AgentPID = sess.PID()
	s.db.Update(task) //nolint:errcheck

	if reattached {
		uxlog.Log("[api] resume: healed task=%s pre_status=%s pid=%d", id, prevStatus, task.AgentPID)
	} else {
		uxlog.Log("[api] resume: started task=%s pid=%d resume=%t", id, task.AgentPID, resume)
	}

	resp := map[string]any{
		"status":     "resumed",
		"pid":        task.AgentPID,
		"session_id": task.SessionID,
	}
	if reattached {
		resp["healed"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Delete Task ---

// deleteTaskAfterPrecheck is a test seam run between handleDeleteTask's
// graduation pre-check and the refusal-capable db.Delete.
var deleteTaskAfterPrecheck func(id string)

// handleDeleteTask is a per-task destructive endpoint. Auth: requires a valid
// token but NOT master. Under the single-tier model only the RCE/credential
// denylist stays master-only: backends CRUD, self-update, and token
// mint/revoke/list.
func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}

	// A graduate's restart intent must survive: refuse before any side effect.
	if s.db.GraduationPending(id) {
		uxlog.Log("[resident] api delete refused task=%s: graduation pending", id)
		writeErr(w, http.StatusConflict, "", db.ErrGraduationPending)
		return
	}

	if deleteTaskAfterPrecheck != nil {
		deleteTaskAfterPrecheck(id)
	}

	// Delete the row FIRST: db.Delete refuses atomically while a graduation
	// restart is pending, and a graduation can commit after the pre-check
	// above. Only on success do we stop the session and remove its log and
	// artifacts, so a refusal never leaves a stopped session / deleted log.
	if err := s.db.Delete(id); err != nil {
		if errors.Is(err, db.ErrGraduationPending) {
			uxlog.Log("[resident] api delete refused task=%s: graduation pending (raced the pre-check)", id)
			writeErr(w, http.StatusConflict, "", err)
			return
		}
		if errors.Is(err, db.ErrResidentProtected) {
			uxlog.Log("[resident] api delete refused task=%s: resident is protected", id)
			writeErr(w, http.StatusConflict, "", err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}

	// Stop the session if running.
	_ = s.runner.Stop(id)

	// Remove session log file.
	os.Remove(agent.SessionLogPath(id)) //nolint:errcheck,gosec // G304: id was validated by db.Get above; SessionLogPath roots at ~/.argus/sessions/<id>.log

	// Remove registered artifacts (manifest rows + on-disk bytes). Best-effort:
	// a residual dir or row is harmless (orphaned, unreachable without a task)
	// but we clear both so a deleted task leaves nothing behind. ArtifactsDir
	// roots at ~/.argus/artifacts/<id>; id was validated by db.Get above.
	if n, derr := s.db.DeleteArtifactsForTask(id); derr != nil {
		uxlog.Log("[api] delete: artifact row cleanup failed: id=%s err=%v", id, derr)
	} else if n > 0 {
		uxlog.Log("[api] delete: cleared %d artifact row(s) for id=%s", n, id)
	}
	os.RemoveAll(agent.ArtifactsDir(id)) //nolint:errcheck,gosec // G304: id validated by db.Get; ArtifactsDir roots at ~/.argus/artifacts/<id>

	events.Emit(model.EventTypeTaskDeleted, id, nil)
	// Drop any per-task cache entry so deleted tasks don't accumulate in
	// the long-lived daemon process.
	s.invalidateColsCache(id)

	// Clean up worktree and branch in background — git operations can take seconds.
	// Mirrors tui.App.deleteTask so worktrees don't linger as orphans until the next
	// completed-task prune sweep.
	cfg := s.db.Config()
	worktree, branch := task.Worktree, task.Branch
	uxlog.Log("[api] delete: task=%s name=%q worktree=%q branch=%q", id, task.Name, worktree, branch)
	go func() {
		repoDir := agent.ResolveDir(task, cfg)
		if worktree != "" {
			agent.RemoveWorktreeAndBranch(worktree, branch, repoDir)
		} else if branch != "" && repoDir != "" {
			agent.DeleteBranch(repoDir, branch)
			agent.DeleteRemoteBranch(repoDir, branch)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Archive / Unarchive ---

func (s *Server) handleArchiveTask(w http.ResponseWriter, r *http.Request) {
	s.setArchive(w, r, true)
}

func (s *Server) handleUnarchiveTask(w http.ResponseWriter, r *http.Request) {
	s.setArchive(w, r, false)
}

func (s *Server) setArchive(w http.ResponseWriter, r *http.Request, archived bool) {
	id := r.PathValue("id")
	defer s.taskLocks.Lock(id)()
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	task.SetArchived(archived)
	if err := s.db.Update(task); err != nil {
		if errors.Is(err, db.ErrGraduationPending) {
			writeErr(w, http.StatusConflict, "", err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	// Mirror the MCP task_archive cleanup so the archive surface stays
	// symmetric across entrypoints. Best-effort; a delete error is logged
	// but does not roll the archive back.
	if archived {
		if n, derr := s.db.DeleteMessagesForTask(id); derr != nil {
			//nolint:gosec // G706: id is the path param already validated against db.Get above; not user-injectable log content.
			log.Printf("api: archive message cleanup failed: id=%s err=%v", id, derr)
		} else if n > 0 {
			//nolint:gosec // G706: id is the path param already validated against db.Get above; not user-injectable log content.
			log.Printf("api: archive cleared %d message(s) for id=%s", n, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"archived": archived})
}

// --- Pin ---

type pinReq struct {
	Pinned *bool `json:"pinned"`
}

// handlePinTask flips only the pinned/archived section flags via the targeted
// db.SetPinned write, so concurrent edits to other columns survive. Pinning an
// archived task restores it (model.Task.SetPinned semantics). It never starts
// or stops a session.
func (s *Server) handlePinTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req pinReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "expected one JSON value within 1 KiB", err)
		return
	}

	if req.Pinned == nil {
		writeErr(w, http.StatusBadRequest, "pinned is required", nil)
		return
	}
	if err := s.db.SetPinned(id, *req.Pinned); err != nil {
		if errors.Is(err, db.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, "task not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	uxlog.Log("[api] pin: task=%s pinned=%t archived=%t", id, task.Pinned, task.Archived)
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "pinned": task.Pinned, "archived": task.Archived})
}

// --- Rename ---

type renameReq struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	var req renameReq
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required", nil)
		return
	}
	// Targeted rename — avoids racing concurrent status changes from the
	// agent process. Mirrors the MCP task_rename and TUI rename modal paths.
	if err := s.db.Rename(task.ID, name); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// --- Set Status ---

type statusSetReq struct {
	Status string `json:"status"`
}

func (s *Server) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	var req statusSetReq
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	var st model.Status
	switch req.Status {
	case "pending":
		st = model.StatusPending
	case "in_progress":
		st = model.StatusInProgress
	case "in_review":
		st = model.StatusInReview
	case "complete":
		st = model.StatusComplete
	default:
		writeErr(w, http.StatusBadRequest, "unknown status: "+req.Status, nil)
		return
	}
	task.SetStatus(st)
	if err := s.db.Update(task); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": task.Status.String()})
}

// --- Stop All ---

func (s *Server) handleStopAll(w http.ResponseWriter, r *http.Request) {
	// Any authenticated token. Single-tier auth: only the RCE/credential
	// denylist (backends CRUD, self-update, token mint/revoke) stays
	// master-only.
	s.runner.StopAll()
	// Mark every running task as in_review.
	tasks, err := s.db.Tasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	stopped := 0
	for _, t := range tasks {
		if t.Status == model.StatusInProgress {
			t.SetStatus(model.StatusInReview)
			s.db.Update(t) //nolint:errcheck
			stopped++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": stopped})
}

// --- Prune Completed ---

// handlePruneCompleted removes every task with status=complete, stops their
// sessions, deletes session logs, and cleans up their worktrees + branches.
// Also sweeps orphaned worktree directories under ~/.argus/worktrees that no
// longer correspond to any DB row. Mirrors the TUI's Ctrl+R action.
//
// Destructive and cross-task, but only deletes already-completed tasks, so
// open to any authenticated token under the single-tier model.
func (s *Server) handlePruneCompleted(w http.ResponseWriter, r *http.Request) {
	cfg := s.db.Config()
	projects := make(map[string]string, len(cfg.Projects))
	for name, p := range cfg.Projects {
		projects[name] = p.Path
	}

	plan, err := agent.PruneCompleted(s.db, agent.PruneOptions{
		WtRoot:   filepath.Join(db.DataDir(), "worktrees"),
		Projects: projects,
		ResolveRepoDir: func(t *model.Task) string {
			return agent.ResolveDir(t, cfg)
		},
		Runner: s.runner,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}

	for _, t := range plan.Pruned {
		events.Emit(model.EventTypeTaskDeleted, t.ID, nil)
	}
	uxlog.Log("[api] prune-completed: pruned=%d worktrees=%d orphans=%d skippedHeraBound=%d",
		len(plan.Pruned), plan.WorktreeCount, plan.OrphanCount, plan.SkippedHeraBound)

	writeJSON(w, http.StatusOK, map[string]any{
		"pruned":           len(plan.Pruned),
		"worktrees":        plan.WorktreeCount,
		"orphans":          plan.OrphanCount,
		"skippedHeraBound": plan.SkippedHeraBound,
	})
}

// --- Fork ---

type forkReq struct {
	Name    string `json:"name"`
	Prompt  string `json:"prompt"`
	Project string `json:"project"`
}

func (s *Server) handleForkTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, err := s.db.Get(id)
	if err != nil || src == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	var req forkReq
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.Name + "-fork"
	}
	prompt := req.Prompt
	if prompt == "" {
		prompt = src.Prompt
	}
	project := req.Project
	if project == "" {
		project = src.Project
	}
	if project == "" {
		writeErr(w, http.StatusBadRequest, "project is required (source task has no project)", nil)
		return
	}
	// Fork name is structured ("<src>-fork" or user-typed); never auto-rename.
	// Forks inherit the source task's backend, model, sandbox override, and account so
	// the new task starts with the same agent and confinement rather than
	// silently defaulting back to the global setting.
	task, err := s.createTask(name, prompt, project, src.Backend, src.Model, src.SandboxOverride, agent.InheritableAccount(src), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	// task.forked is plugin-visible context — it carries the parent linkage
	// the task.created emit (from db.Add inside createTask) cannot, so a
	// fork-aware plugin doesn't have to reconstruct the parent by name.
	events.Emit(model.EventTypeTaskForked, task.ID, map[string]string{
		"from_task_id": src.ID,
		"to_task_id":   task.ID,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     task.ID,
		"name":   task.Name,
		"status": task.Status.String(),
	})
}

// --- Get Output ---

func (s *Server) handleGetOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Parse optional tail size (default 32KB, max 1MB).
	// 1MB ceiling is mirrored on the SPA side as STATIC_OUTPUT_MAX_BYTES
	// (currently 512KB) in static/index.html. If the SPA constant is bumped
	// past 1<<20 the request silently truncates here — bump this cap in
	// lockstep, otherwise scrollback reappears short.
	tailSize := 32 * 1024
	if n, err := strconv.Atoi(r.URL.Query().Get("bytes")); err == nil && n > 0 {
		tailSize = min(n, 1<<20)
	}

	clean := r.URL.Query().Get("clean") == "1"

	// Always read from the on-disk session log when available, even for live
	// sessions. The log file holds the complete history; the in-memory ring
	// buffer only retains the last 256KB. For active in_progress tasks the
	// SPA pairs this read with /stream?since=<X-Output-Total>, where the
	// /stream endpoint replays only the (tiny) ring delta between this read
	// and the live attach. Together they give the client full history with
	// no gap and no duplicates.
	logPath := agent.SessionLogPath(id)
	// id comes from the URL path but is constrained: SessionLogPath does
	// filepath.Join(~/.argus/sessions, id+".log"), and `id` originates as
	// a server-generated nanosecond timestamp string (see model.Task.ID).
	// A `..` segment in the URL would produce a path like ".._.log" inside
	// the sessions dir, not an escape. Single-user local daemon, but the
	// invariant should be held even if a future feature accepts user IDs.
	f, err := os.Open(logPath) //nolint:gosec // logPath rooted at ~/.argus/sessions; id is server-generated
	if err != nil {
		// Live session present but log unavailable (best-effort log creation
		// failed at session start) — fall back to the ring buffer so the
		// client sees the recent tail rather than a 404.
		if sess := s.runner.Get(id); sess != nil {
			// Atomic snapshot: data and ringTotal MUST come from the same
			// lock acquisition. Reading them in two calls lets readLoop
			// advance ringTotal past the bytes in data, leaving the client
			// with a since-cursor that skips bytes never delivered.
			data, ringTotal := sess.RecentOutputTailWithTotal(tailSize)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("X-Source", "ring")
			// data is the ring tail and ringTotal is the high-water mark; the
			// client now has bytes [ringTotal-len(data) .. ringTotal]. Setting
			// since=ringTotal on /stream attaches live with no replay (the
			// caller-already-has-it region matches exactly). Any bytes the
			// session writes between this header and the /stream open are
			// covered by AddWriterFrom's same-lock replay.
			w.Header().Set("X-Output-Total", strconv.FormatUint(ringTotal, 10))
			if clean {
				w.Write([]byte(sanitize.CleanPTYOutput(string(data)))) //nolint:errcheck,gosec // text/plain endpoint; bytes are PTY output for the local single-user daemon, not HTML
			} else {
				w.Write(data) //nolint:errcheck,gosec // text/plain endpoint; bytes are PTY output for the local single-user daemon, not HTML
			}
			return
		}
		writeErr(w, http.StatusNotFound, "no output available", nil)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	fileSize := info.Size()
	startOff := fileSize - int64(tailSize)
	if startOff < 0 {
		startOff = 0
	}
	// Always >= 0 by construction (startOff is clamped to [0, fileSize]).
	// The same is asserted indirectly by io.CopyN below — it errors on a
	// negative count, but we never produce one here.
	bytesToRead := fileSize - startOff

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// X-Output-Total advertises the byte position the client should pass back
	// as ?since=<n> on the /stream endpoint to resume without overlap. We
	// pin it to the file size we observed under Stat() and bound the body
	// read to the same value (io.CopyN), so the count of bytes returned ==
	// X-Output-Total - startOff exactly. Without the bound, a live session
	// growing the file mid-read would leave the client's `since` short of
	// the bytes they actually have.
	w.Header().Set("X-Output-Total", strconv.FormatInt(fileSize, 10))
	if sess := s.runner.Get(id); sess != nil {
		w.Header().Set("X-Source", "live")
	} else {
		w.Header().Set("X-Source", "log")
	}

	if _, err := f.Seek(startOff, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	if clean {
		raw, err := io.ReadAll(io.LimitReader(f, bytesToRead))
		if err != nil {
			// Headers (including X-Output-Total) are already sent; we
			// can't downgrade to 500. Returning silently truncates the
			// 200 response; the SPA's `since` will then point past the
			// bytes actually delivered. Rare in practice — io.ReadAll
			// from a local file fails only on OS errors — but worth
			// flagging if anyone hits the truncation symptom.
			return
		}
		w.Write([]byte(sanitize.CleanPTYOutput(string(raw)))) //nolint:errcheck
	} else {
		io.CopyN(w, f, bytesToRead) //nolint:errcheck
	}
}

// --- Get Links ---

// linksReadCap bounds the bytes we scan for URLs. The full session log can be
// many MB on long-running tasks; capping at 1MB matches the upper bound of
// the /output endpoint and keeps the worst-case allocation predictable.
const linksReadCap = 1 << 20

// handleGetLinks returns http/https URLs extracted from the task's terminal
// output (live ring buffer if running, otherwise the on-disk session log).
// Mirrors the TUI's ctrl+l fuzzy link picker so the web app can offer the
// same "Open link" affordance.
func (s *Server) handleGetLinks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var raw []byte
	// Live session: the full ring buffer is bounded by RingBuffer capacity,
	// so we don't need to cap further here.
	if sess := s.runner.Get(id); sess != nil {
		raw = sess.RecentOutputTail(linksReadCap)
	} else {
		// Fallback: read the tail of the session log file. Older content
		// beyond linksReadCap is dropped so a multi-MB log doesn't pin
		// memory just to enumerate links.
		logPath := agent.SessionLogPath(id)
		f, err := os.Open(logPath) //nolint:gosec // logPath is filepath.Join(~/.argus/sessions, id+".log"); same pattern as handleGetOutput
		if err != nil {
			// Intentionally 200 + empty list (vs handleGetOutput's 404):
			// the picker's caller just wants to render "No links found"
			// uniformly, whether the task doesn't exist or simply hasn't
			// emitted a URL yet. A 404 would force the JS into a bespoke
			// error path for what is, from the user's perspective, the
			// same outcome.
			writeJSON(w, http.StatusOK, map[string]any{"links": []links.Link{}})
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", err)
			return
		}
		offset := info.Size() - int64(linksReadCap)
		if offset < 0 {
			offset = 0
		}
		f.Seek(offset, io.SeekStart) //nolint:errcheck
		raw, err = io.ReadAll(f)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", err)
			return
		}
	}

	out := links.Extract(string(raw))
	if out == nil {
		out = []links.Link{}
	}
	uxlog.Log("[api] task=%s links extracted: %d", id, len(out))
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

// --- Write Input ---

func (s *Server) handleWriteInput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}

	defer s.taskLocks.Lock(id)()

	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
		return
	}
	if task.Archived {
		uxlog.Log("[api] input: rejected archived task=%s origin=%s", id, authOrigin(r))
		writeCodedErrCause(w, http.StatusConflict, "task is archived", "task_archived", nil)
		return
	}

	sess := s.runner.Get(id)
	if sess == nil {
		writeCodedErrCause(w, http.StatusNotFound, "no active session", "no_active_session", nil)
		return
	}

	// X-Input-Origin distinguishes a real keystroke from argus-injected
	// input (see apiclient.Client.WriteInput / agentview.InputOrigin). Any
	// value other than the recognized "system" — including the header's
	// total absence, which is every pre-unify-writeinput-origin client and
	// every plugin token that never learns about this header — resolves to
	// agentview.OriginUser, the zero value and the only behavior this
	// endpoint ever had.
	writeOrigin := agentview.OriginUser
	if r.Header.Get("X-Input-Origin") == "system" {
		writeOrigin = agentview.OriginSystem
	}

	if _, err := sess.WriteInput(data, writeOrigin); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}

	// Audit tag for /input as a stable plugin-callable surface (PR 5 of the
	// plugin substrate). `origin` mirrors the auth middleware's tagging —
	// `master`, `device`, or `scope:<plugin>` — so writes from a plugin token
	// are attributable post-hoc. Distinct from writeOrigin above (who typed
	// it vs. which credential sent it).
	uxlog.Log("[api] input task=%s origin=%s write_origin=%s bytes=%d", id, authOrigin(r), writeOrigin, len(data))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "bytes": strconv.Itoa(len(data))})
}

// --- PTY Size / Resize ---

type sizeResponse struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (s *Server) handleGetSize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := s.runner.Get(id)
	if sess == nil {
		writeErr(w, http.StatusNotFound, "no active session", nil)
		return
	}
	cols, rows := sess.PTYSize()
	writeJSON(w, http.StatusOK, sizeResponse{Cols: cols, Rows: rows})
}

type resizeReq struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
	// ScrolledUp / ComposerDirty are the client-side keep-place signals the
	// daemon cannot see (xterm.js / SwiftTerm scroll position, typed-since-
	// submit over bytes the client sent). Absent means false.
	ScrolledUp    bool `json:"scrolled_up,omitempty"`
	ComposerDirty bool `json:"composer_dirty,omitempty"`
}

// kickClientState carries the keep-place signals a client reported with a
// resize.
type kickClientState struct {
	ScrolledUp    bool
	ComposerDirty bool
}

// kickDeferClock records when kicks at one width were first deferred.
type kickDeferClock struct {
	cols uint16
	at   time.Time
}

func (s *Server) handleResize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := s.runner.Get(id)
	if sess == nil {
		writeErr(w, http.StatusNotFound, "no active session", nil)
		return
	}
	var req resizeReq
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	if req.Cols == 0 || req.Rows == 0 {
		writeErr(w, http.StatusBadRequest, "cols and rows must be > 0", nil)
		return
	}
	// Sanity bounds — terminals beyond these are pathological.
	if req.Cols > 1000 || req.Rows > 1000 {
		writeErr(w, http.StatusBadRequest, "cols/rows out of range", nil)
		return
	}
	if err := sess.Resize(req.Rows, req.Cols); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}

	// After a successful SIGWINCH, decide whether to also trigger a
	// kill+resume rerender. SIGWINCH alone re-flows live UI but leaves
	// scrollback baked at the session's start width — viewers that opened
	// at a different width than the agent committed at see jagged history.
	// The kick stops the session; the runner's exit goroutine restarts it
	// at the new dimensions via --session-id so the agent re-emits the
	// entire conversation. Best-effort: never let this fail the resize.
	rerendered, deferred := s.evalKickRerender(id, req.Rows, req.Cols, kickClientState{ScrolledUp: req.ScrolledUp, ComposerDirty: req.ComposerDirty})

	writeJSON(w, http.StatusOK, struct {
		Cols       int    `json:"cols"`
		Rows       int    `json:"rows"`
		Rerendered bool   `json:"rerendered"`
		Deferred   string `json:"deferred"`
	}{Cols: int(req.Cols), Rows: int(req.Rows), Rerendered: rerendered, Deferred: deferred})
}

// isRedundantResize caches the latest cols for taskID and reports
// whether the previous call already saw the same value. xterm.js fires
// /resize on every terminal mount even when the viewport didn't change, so
// without this gate a reopen of the web agent view would re-evaluate the
// kick predicate and kill any in-flight Claude UI (e.g. AskUserQuestion
// overlays) that the --session-id restart can't rehydrate. Always updates
// the cache so genuine viewport resizes (cols different from the cached
// value) fall through to the predicate.
func (s *Server) isRedundantResize(taskID string, cols uint16) bool {
	s.lastResizeMu.Lock()
	defer s.lastResizeMu.Unlock()
	prev, seen := s.lastResizeCols[taskID]
	s.lastResizeCols[taskID] = cols
	return seen && prev == cols
}

// invalidateColsCache clears the cached cols for taskID so the next
// maybeKickRerender call at any cols re-evaluates the predicate. Called
// from every non-Skip "could have kicked but didn't" outcome (busy
// session, kick attempt error, transient DB error) so subsequent /resize
// calls at the same cols retry instead of permanently short-circuiting.
func (s *Server) invalidateColsCache(taskID string) {
	s.lastResizeMu.Lock()
	delete(s.lastResizeCols, taskID)
	s.lastResizeMu.Unlock()
}

// maybeKickRerender evaluates the shared rerender predicate and, if it fires,
// queues a daemon-side stop+restart. Returns true when a kick was queued.
//
// Phone rotations fan out many resize events — keep the cheap session reads
// before the DB hit so the common no-op case skips the SQLite round-trip.
//
// Safe to call from any goroutine — KickRerender itself only touches runner
// state and the session's PTY.
func (s *Server) maybeKickRerender(taskID string, rows, cols uint16) bool {
	kicked, _ := s.evalKickRerender(taskID, rows, cols, kickClientState{})
	return kicked
}

// evalKickRerender is maybeKickRerender plus the client-reported keep-place
// state (kick-timing-keep-place for the web/macOS clients). deferred names the
// keep-place gate that held an otherwise-eligible kick ("draft" or "scrolled")
// so the client knows it owes a same-size retry; it is empty otherwise.
func (s *Server) evalKickRerender(taskID string, rows, cols uint16, cs kickClientState) (kicked bool, deferred string) {
	sess := s.runner.Get(taskID)
	if sess == nil || !sess.Alive() {
		return false, ""
	}
	if s.runner.HasPendingRestart(taskID) {
		return false, ""
	}
	// Skip when cols matches the most recent resize for this task. xterm.js
	// fires /resize on every terminal mount — without this gate, reopening
	// the web agent view kills any in-flight Claude UI (e.g. AskUserQuestion
	// overlays) that doesn't survive the --session-id restart. Genuine
	// resizes fall through because cols differs from the cached value.
	if s.isRedundantResize(taskID, cols) {
		return false, ""
	}
	// Margin gate before the DB read — eliminates a SQLite hit on every
	// resize that doesn't cross the rerender threshold. The unchanged-cols
	// gate above is cheaper still (mutex-protected map op) and short-
	// circuits before this gate even reads InitialPTYSize.
	initCols, _ := sess.InitialPTYSize()
	if !agent.MarginExceedsRerenderThreshold(initCols, int(cols)) {
		return false, ""
	}
	// A pending graduate's restart belongs to the daemon graduation watcher
	// (stray-job stop first, one restart). A width kick here would race it, so
	// skip — fail closed: an unreadable marker counts as pending. The cols
	// cache is invalidated so a resize after the graduation settles retries.
	if pending, perr := s.db.GraduationPendingErr(taskID); perr != nil || pending {
		uxlog.Log("[resident] kick skipped: graduation pending task=%s (err=%v)", taskID, perr)
		s.invalidateColsCache(taskID)
		return false, ""
	}
	// Now the expensive lookups. Re-check IsIdle here so the idle gate
	// reflects state at-the-moment-of-stop, not earlier.
	if !sess.IsIdle() {
		// Agent is mid-tool-call — invalidate so the next same-cols
		// /resize re-evaluates when the agent goes idle.
		s.invalidateColsCache(taskID)
		return false, ""
	}
	if agent.BlockedOnPrompt(sess) {
		// Agent is idle because it's blocked on a user prompt (AskUserQuestion
		// overlay, permission, plan-mode confirm). A kick would stop+restart
		// and silently dismiss the prompt the user is about to answer.
		// Invalidate so a later resize re-evaluates once the agent moves on.
		s.invalidateColsCache(taskID)
		return false, ""
	}
	// Keep-place gates (kick-timing-keep-place): same order as the TUI's
	// decision — an unsent draft outranks scroll position. With the setting off
	// every client signal is ignored and no screen is read.
	if s.db.Config().UI.RerenderKeepPlace {
		if hold, reason := s.keepPlaceHold(taskID, sess, cols, cs); hold {
			s.invalidateColsCache(taskID)
			return false, reason
		}
	}
	s.clearKickDefer(taskID)
	task, err := s.db.Get(taskID)
	if err != nil || task == nil {
		// Transient DB error — invalidate so the next same-cols /resize
		// retries instead of permanently short-circuiting on a stale
		// cached value.
		s.invalidateColsCache(taskID)
		return false, ""
	}
	if task.SessionID == "" {
		// Codex and other backends without --session-id can never be
		// kicked — leave the cache populated so same-cols /resize
		// permanently short-circuits (correct; no retry semantic).
		return false, ""
	}
	cfg := s.db.Config()
	if available, err := agent.ResumableSessionAvailable(task, cfg); err != nil || !available {
		uxlog.Log("[rerender] deferred task=%s session=%s: transcript unavailable err=%v", task.ID, task.SessionID, err)
		//nolint:gosec // G706: slog encodes these as structured attributes, escaping control characters.
		slog.Warn("[rerender] deferred: transcript unavailable", "task", task.ID, "session", task.SessionID, "error", err)
		s.invalidateColsCache(taskID)
		return false, ""
	}
	if err := agent.KickWidthRerender(s.runner, task, cfg, rows, cols); err != nil {
		// Don't fail the resize on a kick failure — runner.KickRerender's
		// own slog line records the task; just note the failure here.
		log.Printf("api: KickRerender failed: %v", err)
		// Kick attempt failed — invalidate so the next same-cols /resize
		// retries (a transient daemon error shouldn't permanently break
		// the gate).
		s.invalidateColsCache(taskID)
		return false, ""
	}
	// runner.KickRerender already logs via slog with task=, cols=, rows=.
	if err := s.db.SetStatus(taskID, model.StatusInProgress); err != nil {
		uxlog.Log("[rerender] restart status write failed task=%s err=%v", taskID, err)
		//nolint:gosec // G706: slog encodes these as structured attributes, escaping control characters.
		slog.Warn("[rerender] restart status write failed", "task", taskID, "err", err)
	}
	return true, ""
}

// --- Stream Output (SSE) ---

func (s *Server) handleStreamOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sess := s.runner.Get(id)
	if sess == nil {
		writeErr(w, http.StatusNotFound, "no active session", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported", nil)
		return
	}

	// Optional resume cursor. Clients first read /output (which returns
	// X-Output-Total: N from the on-disk log size) and then open this stream
	// with ?since=N. AddWriterFrom replays only the ring delta between N and
	// the current total under a single lock acquisition, then attaches live
	// — no overlap with the disk-log bytes the client already has, no gap.
	// Absent or invalid `since` defaults to 0, which replays the full ring
	// (matches the legacy AddWriter behaviour). `since` > currentTotal is
	// also valid: AddWriterFrom skips the replay block entirely and attaches
	// live — the client is "ahead" of the ring (e.g., reconnected with a
	// stale-cached cursor), so no replay is the correct outcome.
	var since uint64
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			since = n
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// Use a channelWriter so AddWriterFrom's synchronous ring buffer replay
	// doesn't block readLoop. The channel is large enough to hold the full
	// 256KB ring buffer replay (256KB / 4KB chunks = 64 items), and Write
	// drops on full to keep the lock-held replay non-blocking.
	cw := &channelWriter{ch: make(chan []byte, 128)}
	sess.AddWriterFrom(cw, since)
	defer sess.RemoveWriter(cw)

	// Subscribe to agent-staged clipboard updates for this task. Any change
	// (set or clear) queues a `clipboard` SSE event. The subscriber callback
	// runs on the goroutine that called Set/Clear — must not block, so a
	// buffered channel + drop-on-full keeps the producer fast. text and
	// present travel as a single struct so a partial drop can never desync
	// the two halves (an earlier two-channel design could pair a fresh text
	// with a stale present flag during bursty updates).
	clipCh := make(chan clipEvent, 16)
	var unsubClip func()
	if s.clipboard != nil {
		unsubClip = s.clipboard.Subscribe(id, func(u clipboard.Update) {
			select {
			case clipCh <- clipEvent{text: u.Text, present: u.Present}:
			default:
			}
		})
		defer unsubClip()
		// Emit current state on connect so a freshly-opened tab catches a
		// payload that was staged before the SSE was established.
		if text, ok := s.clipboard.Get(id); ok {
			fmt.Fprintf(w, "event: clipboard\ndata: %s\n\n", encodeClipboardEvent(text, true)) //nolint:errcheck
			flusher.Flush()
		}
	}

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	// sess.Done() closes when the session's process exits. Without observing
	// it explicitly, `event: exit` would never fire for the SPA — cw.ch is
	// fed only by readLoop's Write calls, and is never closed when the session
	// dies. The SPA's reconnect-on-rerender path keys off `event: exit`, so
	// missing this signal makes the rerender recovery unobservable.
	sessDone := sess.Done()

	for {
		select {
		case data, ok := <-cw.ch:
			if !ok {
				fmt.Fprintf(w, "event: exit\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			fmt.Fprintf(w, "data: %s\n\n", encoded)
			flusher.Flush()
		case ev := <-clipCh:
			fmt.Fprintf(w, "event: clipboard\ndata: %s\n\n", encodeClipboardEvent(ev.text, ev.present)) //nolint:errcheck
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case <-sessDone:
			// Drain any queued bytes that arrived before exit so the client
			// sees the agent's last words. Non-blocking — only takes what's
			// already buffered in the channel.
			for {
				select {
				case data := <-cw.ch:
					encoded := base64.StdEncoding.EncodeToString(data)
					fmt.Fprintf(w, "data: %s\n\n", encoded) //nolint:errcheck
					flusher.Flush()
				default:
					goto drained
				}
			}
		drained:
			// Embed the rerender hint in the exit payload so the SPA can
			// distinguish "session ended" from "kick-restart in flight"
			// without depending on the /resize POST's .then() racing the
			// SSE delivery. Daemon already evaluated HasPendingRestart
			// for its own skip-transition decision; surface it here too.
			exitPayload := `{"rerendering":false}`
			if s.runner.HasPendingRestart(id) {
				exitPayload = `{"rerendering":true}`
			}
			fmt.Fprintf(w, "event: exit\ndata: %s\n\n", exitPayload) //nolint:errcheck
			flusher.Flush()
			return
		case <-r.Context().Done():
			return
		}
	}
}

// clipEvent bundles the text and presence flag for a clipboard SSE update so
// they travel through a single channel and can never desync under burst load.
type clipEvent struct {
	text    string
	present bool
}

// encodeClipboardEvent renders the JSON body of a clipboard SSE event.
// `{"text":"…"}` when a payload is present, `{"cleared":true}` otherwise.
func encodeClipboardEvent(text string, present bool) string {
	if !present {
		return `{"cleared":true}`
	}
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return `{"cleared":true}`
	}
	return string(body)
}

// channelWriter implements io.Writer by sending copies of written data to a
// buffered channel. This avoids the io.Pipe deadlock when AddWriter replays
// the ring buffer synchronously — the channel buffer absorbs the full replay.
type channelWriter struct {
	ch chan []byte
}

func (cw *channelWriter) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case cw.ch <- cp:
		return len(p), nil
	default:
		// Channel full — drop data to avoid blocking the session's readLoop.
		return len(p), nil
	}
}

// --- Projects (full CRUD) ---

type projectJSON struct {
	Name    string                  `json:"name"`
	Path    string                  `json:"path"`
	Branch  string                  `json:"branch,omitempty"`
	Backend string                  `json:"backend,omitempty"`
	Sandbox *projectSandboxOverride `json:"sandbox,omitempty"`
	// Profile is a pointer so a request can tell omitted (preserve) from ""
	// (clear); responses always carry it.
	Profile *string `json:"profile,omitempty"`
}

// projectSandboxOverride is a JSON-friendly view of config.ProjectSandboxConfig.
// `enabled` is a *bool because nil means "inherit global"; the JSON encoder
// emits `null` for that, which the SPA renders as an "Inherit" radio.
type projectSandboxOverride struct {
	Enabled          *bool    `json:"enabled"`
	DenyRead         []string `json:"deny_read"`
	ExtraWrite       []string `json:"extra_write"`
	AllowAppleEvents []string `json:"allow_apple_events"`
}

func projectToJSON(name string, p config.Project) projectJSON {
	profile := p.Profile
	out := projectJSON{Name: name, Path: p.Path, Branch: p.Branch, Backend: p.Backend, Profile: &profile}
	if p.Sandbox.Enabled != nil || len(p.Sandbox.DenyRead) > 0 || len(p.Sandbox.ExtraWrite) > 0 || len(p.Sandbox.AllowAppleEvents) > 0 {
		out.Sandbox = &projectSandboxOverride{
			Enabled:          p.Sandbox.Enabled,
			DenyRead:         stringsOrEmpty(p.Sandbox.DenyRead),
			ExtraWrite:       stringsOrEmpty(p.Sandbox.ExtraWrite),
			AllowAppleEvents: stringsOrEmpty(p.Sandbox.AllowAppleEvents),
		}
	}
	return out
}

// invalidProjectWritePath returns an error message for the first project
// sandbox.extra_write entry that fails agent.ValidateWritePath, or "".
func invalidProjectWritePath(req projectJSON) string {
	if req.Sandbox == nil {
		return ""
	}
	for _, p := range req.Sandbox.ExtraWrite {
		if reason := agent.ValidateWritePath(strings.TrimSpace(p)); reason != "" {
			return "invalid extra_write path " + p + ": " + reason
		}
	}
	return ""
}

func projectFromJSON(req projectJSON) config.Project {
	out := config.Project{
		Path:    req.Path,
		Branch:  req.Branch,
		Backend: req.Backend,
	}
	if req.Profile != nil {
		out.Profile = *req.Profile
	}
	if req.Sandbox != nil {
		out.Sandbox.Enabled = req.Sandbox.Enabled
		out.Sandbox.DenyRead = req.Sandbox.DenyRead
		out.Sandbox.ExtraWrite = req.Sandbox.ExtraWrite
		// Filter bundle IDs through the same validator the SBPL emitter
		// uses. Garbage entries would persist as-is otherwise and silently
		// no-op when the profile is generated; rejecting at the API
		// boundary keeps the stored config truthful about what's active.
		cleaned := make([]string, 0, len(req.Sandbox.AllowAppleEvents))
		for _, b := range req.Sandbox.AllowAppleEvents {
			b = strings.TrimSpace(b)
			if agent.IsValidBundleID(b) {
				cleaned = append(cleaned, b)
			}
		}
		out.Sandbox.AllowAppleEvents = cleaned
	}
	return out
}

func (s *Server) handleListProjectsFull(w http.ResponseWriter, r *http.Request) {
	projects, err := s.db.Projects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	out := make([]projectJSON, 0, len(projects))
	for name, p := range projects {
		out = append(out, projectToJSON(name, p))
	}
	// stable order
	sortByName := func(a, b projectJSON) bool { return a.Name < b.Name }
	sortProjects(out, sortByName)
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func sortProjects(items []projectJSON, less func(a, b projectJSON) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// knownProfile reports whether name resolves to a diligence profile: the
// canonical "default" or any profile in the library directory.
func knownProfile(name string) bool {
	if name == "" || name == "default" {
		return true
	}
	loader := &profiles.Loader{LibraryDir: filepath.Join(db.DataDir(), "profiles")}
	for _, n := range loader.Discover() {
		if n == name {
			return true
		}
	}
	return false
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req projectJSON
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Path) == "" {
		writeErr(w, http.StatusBadRequest, "name and path are required", nil)
		return
	}
	if msg := invalidProjectWritePath(req); msg != "" {
		writeErr(w, http.StatusBadRequest, msg, nil)
		return
	}
	if req.Profile != nil && !knownProfile(*req.Profile) {
		writeErr(w, http.StatusBadRequest, "unknown profile", nil)
		return
	}
	if s.db.ProjectInConfigToml(req.Name) {
		writeErr(w, http.StatusConflict, "project is defined in config.toml", nil)
		return
	}
	stored, changed, err := s.db.SaveProject(req.Name, projectFromJSON(req), db.ProjectSaveOptions{})
	if err != nil {
		uxlog.Log("[api] create project %q failed: %v", req.Name, err)
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	if changed {
		events.Emit(model.EventTypeConfigChanged, "", map[string]string{"resource": "projects", "name": req.Name})
		uxlog.Log("[api] project %q created or updated", req.Name)
	} else {
		uxlog.Log("[api] project %q save was a no-op; no event", req.Name)
	}
	writeJSON(w, http.StatusCreated, projectToJSON(req.Name, stored))
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req projectJSON
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	// Path is required on update too.
	if strings.TrimSpace(req.Path) == "" {
		writeErr(w, http.StatusBadRequest, "path is required", nil)
		return
	}
	if msg := invalidProjectWritePath(req); msg != "" {
		writeErr(w, http.StatusBadRequest, msg, nil)
		return
	}
	if req.Profile != nil && !knownProfile(*req.Profile) {
		writeErr(w, http.StatusBadRequest, "unknown profile", nil)
		return
	}
	if s.db.ProjectInConfigToml(name) {
		writeErr(w, http.StatusConflict, "project is defined in config.toml", nil)
		return
	}
	if s.beforeProjectSave != nil {
		s.beforeProjectSave()
	}
	stored, changed, err := s.db.SaveProject(name, projectFromJSON(req), db.ProjectSaveOptions{PreserveProfile: req.Profile == nil, RequireExisting: true})
	if errors.Is(err, db.ErrProjectNotFound) {
		writeErr(w, http.StatusNotFound, "project not found", nil)
		return
	}
	if err != nil {
		uxlog.Log("[api] update project %q failed: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	if changed {
		events.Emit(model.EventTypeConfigChanged, "", map[string]string{"resource": "projects", "name": name})
		uxlog.Log("[api] project %q updated", name)
	} else {
		uxlog.Log("[api] project %q update was a no-op; no event", name)
	}
	writeJSON(w, http.StatusOK, projectToJSON(name, stored))
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.db.DeleteProject(name); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// --- Backends (full CRUD) ---

type backendJSON struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	PromptFlag string `json:"prompt_flag,omitempty"`
	// Model is the backend's default model, delivered through the backend's
	// supported session-start mechanism unless the task carries its own override.
	Model string `json:"model,omitempty"`
	// Models follows the effective backend configuration used at creation.
	Models []string `json:"models,omitempty"`
	// ModelPolicy is "configured", "curated" or "freeform": how an explicit
	// task model is validated for this backend (agent.ModelPolicy).
	ModelPolicy string `json:"model_policy"`
	// Kind, Label, SkillPrefix, and SessionSwitcher describe the backend's
	// family (agent.FamilyOf) so clients never hardcode backend names. Kind
	// is "" for a custom command; Label is the family label only for the
	// backend named after its family, else the backend's own name.
	Kind            string `json:"kind"`
	Label           string `json:"label"`
	SkillPrefix     string `json:"skill_prefix"`
	SessionSwitcher bool   `json:"session_switcher"`
	// AccountMove says whether a task on this backend can change accounts and
	// keep its conversation (POST /api/tasks/{id}/account).
	AccountMove accountMoveJSON `json:"account_move"`
}

type accountMoveJSON struct {
	Supported             bool `json:"supported"`
	PreservesConversation bool `json:"preserves_conversation"`
}

// newBackendJSON builds the wire shape for one configured backend.
func newBackendJSON(name string, b config.Backend) backendJSON {
	fam := agent.FamilyOf(b.Command)
	// The family label names only the backend that IS the family (name
	// "codex" → "Codex"); other instances keep their own name so two
	// backends of one family (claude, claude-haiku) stay distinguishable.
	label := name
	if name == string(fam.Kind) && fam.Label != "" {
		label = fam.Label
	}
	return backendJSON{
		Name:            name,
		Command:         b.Command,
		PromptFlag:      b.PromptFlag,
		Model:           b.Model,
		Models:          agent.BackendModels(b),
		ModelPolicy:     agent.ModelPolicy(b),
		Kind:            string(fam.Kind),
		Label:           label,
		SkillPrefix:     fam.SkillPrefix,
		SessionSwitcher: fam.SessionSwitcher,
		AccountMove: accountMoveJSON{
			Supported:             fam.TransferSession != nil,
			PreservesConversation: fam.TransferSession != nil,
		},
	}
}

func (s *Server) handleListBackends(w http.ResponseWriter, r *http.Request) {
	backends, err := s.db.Backends()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	merged := s.db.Config().Backends
	out := make([]backendJSON, 0, len(backends))
	for name, b := range backends {
		if mb, ok := merged[name]; ok {
			b = mb
		}
		out = append(out, newBackendJSON(name, b))
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	out = append(out, backendJSON{Name: config.DynamicBackend, Kind: config.DynamicBackend, Label: "Dynamic"})
	writeJSON(w, http.StatusOK, map[string]any{"backends": out})
}

// handleCreateBackend persists a new backend definition. Master-only (RCE
// denylist) — a backend is a command template, so write access is equivalent
// to arbitrary command execution on the host the next time a task spawns.
// Unlike project CRUD (open under single-tier auth), this stays gated.
func (s *Server) handleCreateBackend(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	var req backendJSON
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Command) == "" {
		writeErr(w, http.StatusBadRequest, "name and command are required", nil)
		return
	}
	if req.Name == config.DynamicBackend {
		writeErr(w, http.StatusBadRequest, "reserved backend name", nil)
		return
	}
	b := config.Backend{Command: req.Command, PromptFlag: req.PromptFlag, Model: req.Model}
	if err := s.db.SetBackend(req.Name, b); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusCreated, newBackendJSON(req.Name, b))
}

// handleUpdateBackend overwrites the backend keyed by path-param name.
// Master-only.
func (s *Server) handleUpdateBackend(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	name := r.PathValue("name")
	if name == config.DynamicBackend {
		writeErr(w, http.StatusBadRequest, "reserved backend name", nil)
		return
	}
	var req backendJSON
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeErr(w, http.StatusBadRequest, "command is required", nil)
		return
	}
	b := config.Backend{Command: req.Command, PromptFlag: req.PromptFlag, Model: req.Model}
	if err := s.db.SetBackend(name, b); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusOK, newBackendJSON(name, b))
}

// handleDeleteBackend removes a backend from config. Master-only.
func (s *Server) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	name := r.PathValue("name")
	if name == config.DynamicBackend {
		writeErr(w, http.StatusBadRequest, "reserved backend name", nil)
		return
	}
	if err := s.db.DeleteBackend(name); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// handleGetConfig returns the full config.Config snapshot — superset of
// what's exposed by /api/settings. The TUI calls db.Config() in many
// places (project lookup, default backend, sandbox state, UI prefs); this
// endpoint lets a remote TUI build the same value over HTTP without
// adding a dozen specialised endpoints. Read-only; open to any authenticated
// token (the backend command templates it discloses are already readable via
// GET /api/backends).
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.db.Config())
}

// handleSessionState exposes the runner's live in-memory state for the TUI's
// session-aware status polling. Without this, the TUI store adapter would
// have to derive idle/running from /api/tasks alone — which reflects DB
// state, not session liveness. Open to device tokens (read-only).
func (s *Server) handleSessionState(w http.ResponseWriter, r *http.Request) {
	running, idle := s.runner.RunningAndIdle()
	writeJSON(w, http.StatusOK, map[string]any{
		"running": running,
		"idle":    idle,
	})
}

// handleHasPendingRestart exposes the runner's per-task pending-restart
// flag — true when a kick-rerender has stopped the session and the runner
// is about to spawn a replacement. The TUI uses this to avoid tearing
// down UI state mid-rerender (mirrors SessionProvider.HasPendingRestart).
// Open to device tokens (read-only).
func (s *Server) handleHasPendingRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeJSON(w, http.StatusOK, map[string]bool{"pending": s.runner.HasPendingRestart(id)})
}

// --- Git status / diff / files ---

type gitStatusResponse struct {
	gitutil.GitStatusRefreshMsg
	GitRevision   string `json:"git_revision"`
	Stale         bool   `json:"stale"`
	WatchDegraded bool   `json:"watch_degraded"`
}

func writeCodeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil || task.Worktree == "" {
		writeErr(w, http.StatusNotFound, "task or worktree not found", nil)
		return
	}
	snap, err := s.wt.status(r.Context(), task.ID, task.Worktree, gitutil.Options{BaseBranch: task.BaseBranch})
	if errors.Is(err, errWtUnavailable) {
		uxlog.Log("[api] git status unavailable task=%s", task.ID)
		writeCodeErr(w, http.StatusServiceUnavailable, "git_status_unavailable", "git status unavailable")
		return
	}
	if err != nil {
		uxlog.Log("[api] git status failed task=%s: %v", task.ID, err)
		writeErr(w, http.StatusInternalServerError, "git status failed", nil)
		return
	}
	msg := snap.msg
	uxlog.Log("[api] git status task=%s base=%q resolved=%q revision=%s stale=%v", task.ID, msg.Comparison.RequestedBase, msg.Comparison.ResolvedBase, snap.revision, snap.stale)
	writeJSON(w, http.StatusOK, gitStatusResponse{GitStatusRefreshMsg: msg, GitRevision: snap.revision, Stale: snap.stale, WatchDegraded: snap.degraded})
}

type gitDiscardRequest struct {
	Path            string `json:"path"`
	Scope           string `json:"scope"`
	DeleteUntracked bool   `json:"delete_untracked"`
}

func (s *Server) handleGitDiscard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req gitDiscardRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeCodeErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Scope == "" {
		req.Scope = "local"
	}
	if req.Scope != "local" {
		writeCodeErr(w, http.StatusBadRequest, "invalid_scope", "scope must be local")
		return
	}

	defer s.taskLocks.Lock(id)()
	task, err := s.db.Get(id)
	if err != nil || task == nil || task.Worktree == "" {
		writeCodeErr(w, http.StatusNotFound, "task_not_found", "task or worktree not found")
		return
	}
	if task.Archived {
		uxlog.Log("[api] git discard rejected archived task=%s", id)
		writeCodeErr(w, http.StatusConflict, "task_archived", "task is archived")
		return
	}
	if _, serr := os.Stat(task.Worktree); serr != nil {
		writeCodeErr(w, http.StatusNotFound, "worktree_not_found", "task or worktree not found")
		return
	}

	opts := gitutil.Options{BaseBranch: task.BaseBranch}
	e := s.wt.register(task.ID, task.Worktree, opts)
	e.mutate.Lock()
	derr := gitutil.DiscardFile(task.Worktree, req.Path, req.DeleteUntracked)
	e.mutate.Unlock()
	if derr != nil {
		var de *gitutil.DiscardError
		if !errors.As(derr, &de) {
			de = &gitutil.DiscardError{Code: gitutil.DiscardGitFailed, Msg: "discard failed"}
		}
		status := http.StatusConflict
		switch de.Code {
		case gitutil.DiscardInvalidPath:
			status = http.StatusBadRequest
		case gitutil.DiscardGitFailed:
			status = http.StatusInternalServerError
		}
		uxlog.Log("[api] git discard rejected task=%s code=%s", task.ID, de.Code)
		writeCodeErr(w, status, de.Code, de.Msg)
		return
	}
	uxlog.Log("[api] git discard ok task=%s", task.ID)
	s.wt.publishDiscard(task.ID, task.Worktree, opts, []string{req.Path})
	writeJSON(w, http.StatusOK, map[string]any{"path": req.Path, "discarded": true, "scope": "local"})
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil || task.Worktree == "" {
		writeErr(w, http.StatusNotFound, "task or worktree not found", nil)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" || strings.Contains(path, "..") || filepath.IsAbs(path) {
		// Reject absolute paths — gitutil.FetchFileDiff falls back to
		// `git diff --no-index <path>` which would otherwise read arbitrary
		// files (e.g. /etc/passwd) for files not in the worktree.
		writeErr(w, http.StatusBadRequest, "valid path query param required", nil)
		return
	}
	msg := gitutil.FetchFileDiff(task.ID, task.Worktree, path, gitutil.Options{BaseBranch: task.BaseBranch})
	if msg.Err != nil {
		uxlog.Log("[api] git diff failed task=%s: %v", task.ID, msg.Err)
		writeErr(w, http.StatusInternalServerError, "git diff failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil || task.Worktree == "" {
		writeErr(w, http.StatusNotFound, "task or worktree not found", nil)
		return
	}
	dir := r.URL.Query().Get("dir")
	if strings.Contains(dir, "..") || filepath.IsAbs(dir) {
		writeErr(w, http.StatusBadRequest, "invalid dir", nil)
		return
	}
	msg := gitutil.FetchDirFiles(task.ID, task.Worktree, dir)
	writeJSON(w, http.StatusOK, msg)
}

// --- Skills ---

type skillJSON struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	filter := r.URL.Query().Get("filter")

	var extraDirs []string
	if project != "" {
		projects, err := s.db.Projects()
		if err != nil {
			log.Printf("[api] handleListSkills: failed to load projects: %v", err)
		} else if p, ok := projects[project]; ok && p.Path != "" {
			extraDirs = []string{filepath.Join(p.Path, ".claude", "skills")}
		}
	}

	configDir, err := s.skillsConfigDir(r.URL.Query().Get("account"), r.URL.Query().Get("task"), project)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	items := skills.WithBuiltins(skills.LoadSkillsFrom(configDir, extraDirs))
	if filter != "" {
		items = skills.FilterSkills(items, filter)
	}

	result := make([]skillJSON, len(items))
	for i, it := range items {
		result[i] = skillJSON{Name: it.Name, Description: it.Description}
	}

	writeJSON(w, http.StatusOK, map[string]any{"skills": result})
}

// skillsConfigDir picks the Claude config dir whose user skills and plugins
// autocomplete should list: an explicit account, else the task's account, else
// the project / global default. "" means ~/.claude; an account without a
// Claude config dir also falls back to it.
func (s *Server) skillsConfigDir(account, taskID, project string) (string, error) {
	cfg := s.db.Config()
	if account == "" && taskID != "" {
		t, err := s.db.Get(taskID)
		if err != nil {
			return "", fmt.Errorf("task %q not found", taskID)
		}
		account = t.Account
		if project == "" {
			project = t.Project
		}
	}
	if account != "" {
		if err := cfg.ValidateAccount(account); err != nil {
			return "", err
		}
	}
	name := cfg.ResolveAccount(account, project)
	dir, explicit, err := cfg.ClaudeConfigDir(name)
	if err != nil {
		uxlog.Log("[account] skills: account %q has no Claude config dir, using default: %v", name, err)
		return "", nil
	}
	if !explicit {
		return "", nil
	}
	return dir, nil
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api] json encode error: %v", err)
	}
}

// writeErr writes a standard `{"error": "..."}` JSON body at the given status.
// It is the single helper behind the error-response sites across internal/api,
// replacing the repeated inline writeJSON(..., map[string]string{"error": ...}).
//
// The emitted message is composed to stay byte-identical to the call sites it
// replaces:
//   - msg == "", err != nil  -> err.Error()            (the bare err.Error() form)
//   - msg != "", err != nil  -> msg + ": " + err.Error() (the "prefix: " + err form)
//   - msg != "", err == nil  -> msg                     (a static message)
//   - msg == "", err == nil  -> ""                      (degenerate; avoid)
//
// writeModelNotAllowed writes the 422 model_not_allowed body and reports
// whether err was a model rejection.
func writeModelNotAllowed(w http.ResponseWriter, err error) bool {
	var me *agent.ModelNotAllowedError
	if !errors.As(err, &me) {
		return false
	}
	allowed := me.AllowedModels
	if allowed == nil {
		allowed = []string{}
	}
	uxlog.Log("[model] create rejected 422 backend=%q model=%q", me.Backend, me.RequestedModel)
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error":           "model is not allowed for resolved backend",
		"code":            "model_not_allowed",
		"backend":         me.Backend,
		"requested_model": me.RequestedModel,
		"allowed_models":  allowed,
	})
	return true
}

func writeErr(w http.ResponseWriter, status int, msg string, err error) {
	text := msg
	switch {
	case err != nil && msg != "":
		text = msg + ": " + err.Error()
	case err != nil:
		text = err.Error()
	}
	writeJSON(w, status, map[string]string{"error": text})
}

// writeCodedErrCause is writeErr plus a stable machine-readable `code` field.
func writeCodedErrCause(w http.ResponseWriter, status int, msg, code string, err error) {
	text := msg
	switch {
	case err != nil && msg != "":
		text = msg + ": " + err.Error()
	case err != nil:
		text = err.Error()
	}
	writeJSON(w, status, map[string]string{"error": text, "code": code})
}

func sanitizeName(prompt string) string {
	// Take first 40 runes, replace newlines with spaces.
	runes := []rune(prompt)
	if len(runes) > 40 {
		runes = runes[:40]
	}
	for i, r := range runes {
		if r == '\n' || r == '\r' || r == '\t' {
			runes[i] = ' '
		}
	}
	return string(runes)
}

// keepPlaceHold evaluates the keep-place gates for a kick that every earlier
// gate (margin, idle, prompt) would fire, and reports whether one holds it.
// reason is "draft" or "scrolled". A hold starts the per-width deferral clock.
//
// The rendered-composer read re-emulates the session's output tail, so it runs
// ONLY here — on the kick-evaluation path, after the cheap gates passed and
// only when no draft-aware bridge answered — never per poll or per resize that
// is not evaluating a kick, so it costs nothing in steady state.
func (s *Server) keepPlaceHold(taskID string, sess agent.SessionHandle, cols uint16, cs kickClientState) (bool, string) {
	in := agent.ComposerDraftInput{TypedFlag: cs.ComposerDirty}
	in.BridgeAware, in.BridgeHas = s.bridgeDraftSignal(taskID)
	if !in.BridgeAware {
		in.ScreenHas, in.ScreenFound = s.composerFromScreen(sess)
	}
	s.kickDeferMu.Lock()
	clock, ok := s.kickDeferSince[taskID]
	if ok && clock.cols == cols {
		in.DeferredFor = time.Since(clock.at)
	}
	s.kickDeferMu.Unlock()

	has, _, capped := agent.ComposerDraft(in)
	if capped {
		uxlog.Log("[rerender] typed-flag deferral cap reached task=%s cols=%d after %s: no longer holding the kick", taskID, cols, in.DeferredFor.Round(time.Second))
	}
	reason := ""
	switch {
	case has:
		reason = "draft"
	case cs.ScrolledUp:
		reason = "scrolled"
	default:
		return false, ""
	}
	s.kickDeferMu.Lock()
	if c, held := s.kickDeferSince[taskID]; !held || c.cols != cols {
		s.kickDeferSince[taskID] = kickDeferClock{cols: cols, at: time.Now()}
	}
	s.kickDeferMu.Unlock()
	if reason == "draft" {
		uxlog.Log("[rerender] deferred task=%s cols=%d: unsent composer draft — preserving typed text", taskID, cols)
	} else {
		uxlog.Log("[rerender] deferred task=%s cols=%d: client scrolled up — preserving the user's place", taskID, cols)
	}
	return true, reason
}

// composerFromScreen reads the rendered Claude composer from the session's
// output tail. found=false (not Claude's composer, or no marker on screen) is
// "unknown", never "empty".
func (s *Server) composerFromScreen(sess agent.SessionHandle) (has, found bool) {
	cols, rows := sess.PTYSize()
	tail := agent.SubstantiveTail(sess.RecentOutputTail, 64*1024, agent.NeedsInputMaxExpandBytes)
	s.kickScreenMu.Lock()
	defer s.kickScreenMu.Unlock()
	draft, found := s.kickScreen.InputDraft(tail, cols, rows)
	return strings.TrimSpace(draft) != "", found
}

// clearKickDefer forgets a task's keep-place deferral clock once a kick is no
// longer held.
func (s *Server) clearKickDefer(taskID string) {
	s.kickDeferMu.Lock()
	delete(s.kickDeferSince, taskID)
	s.kickDeferMu.Unlock()
}
