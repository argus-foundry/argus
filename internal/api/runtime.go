package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	applyNextResume = "next_resume"
	applyRestart    = "restart"
	maxModelLen     = 200
)

var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-\[\]]*$`)

type runtimeSettings struct {
	Backend         string `json:"backend"`
	Model           string `json:"model"`
	SandboxOverride string `json:"sandbox_override"`
	Prompt          string `json:"prompt"`
}

func runtimeOf(t *model.Task) runtimeSettings {
	return runtimeSettings{Backend: t.Backend, Model: t.Model, SandboxOverride: t.SandboxOverride, Prompt: t.Prompt}
}

// runtimeRevision fingerprints the editable runtime fields plus the saved
// session ID, so a concurrent settings edit or a conversation switch makes a
// stale If-Match fail.
func runtimeRevision(t *model.Task) string {
	h := sha256.New()
	for _, f := range []string{t.Backend, t.Model, t.SandboxOverride, t.Prompt, t.SessionID} {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func normalizeIfMatch(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "W/")
	return strings.Trim(v, `"`)
}

func (s *Server) liveRuntimeSession(id string) agent.SessionHandle {
	if h := s.runner.Get(id); h != nil && h.Alive() {
		return h
	}
	return nil
}

// handleGetTaskRuntime serves GET /api/tasks/{id}/runtime.
func (s *Server) handleGetTaskRuntime(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
		return
	}
	sess := s.liveRuntimeSession(id)
	live := sess != nil
	busy := live && !sess.IsIdle()
	avail := []string{}
	if !s.runner.HasPendingRestart(id) {
		if !live {
			avail = append(avail, applyNextResume)
		}
		avail = append(avail, applyRestart)
	}
	rev := runtimeRevision(task)
	w.Header().Set("ETag", `"`+rev+`"`)
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":         id,
		"runtime":         runtimeOf(task),
		"live":            live,
		"busy":            busy,
		"available_apply": avail,
		"revision":        rev,
	})
}

type runtimePatchReq struct {
	Model           *string `json:"model"`
	Backend         *string `json:"backend"`
	SandboxOverride *string `json:"sandbox_override"`
	Prompt          *string `json:"prompt"`
	Apply           string  `json:"apply"`
	Force           bool    `json:"force"`
	ForceRevive     bool    `json:"force_revive"`
}

// handlePatchTaskRuntime serves PATCH /api/tasks/{id}/runtime. Every
// rejection happens before the session is stopped or any field is written.
// A restart that fails after persisting reports the partial outcome through
// writeRestartFailed.
func (s *Server) handlePatchTaskRuntime(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req runtimePatchReq
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024))
	if err := decoder.Decode(&req); err != nil {
		writeCodedErrCause(w, http.StatusBadRequest, "invalid JSON", "invalid_body", err)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeCodedErrCause(w, http.StatusBadRequest, "unexpected trailing data", "invalid_body", err)
		return
	}
	unlock, err := s.lockTaskMutation(r.Context(), id)
	if err != nil {
		uxlog.Log("[api] runtime: task lock canceled task=%s err=%v", id, err)
		writeErr(w, http.StatusServiceUnavailable, "task operation canceled", err)
		return
	}
	defer unlock()
	if err := r.Context().Err(); err != nil {
		uxlog.Log("[api] runtime: request canceled after lock task=%s err=%v", id, err)
		writeErr(w, http.StatusServiceUnavailable, "task operation canceled", err)
		return
	}
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
		return
	}
	if s.refuseGraduationPending(w, id, "runtime") {
		return
	}

	if req.Apply != applyNextResume && req.Apply != applyRestart {
		writeCodedErrCause(w, http.StatusBadRequest, `apply must be "next_resume" or "restart"`, "invalid_apply", nil)
		return
	}
	if req.Model == nil && req.Backend == nil && req.SandboxOverride == nil && req.Prompt == nil {
		writeCodedErrCause(w, http.StatusBadRequest, "no runtime field supplied", "no_changes", nil)
		return
	}
	if req.SandboxOverride != nil && authOrigin(r) != "master" {
		writeCodedErrCause(w, http.StatusForbidden, "master token required to change sandbox_override", "master_required", nil)
		return
	}

	rev := runtimeRevision(task)
	ifMatch := r.Header.Get("If-Match")
	if strings.TrimSpace(ifMatch) == "" {
		writeCodedErrCause(w, http.StatusPreconditionRequired, "If-Match header with the runtime revision is required", "if_match_required", nil)
		return
	}
	if normalizeIfMatch(ifMatch) != rev {
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{
			"error": "runtime revision is stale", "code": "revision_stale", "revision": rev,
		})
		return
	}

	cfg := s.db.Config()
	cand := *task
	if req.Backend != nil {
		cand.Backend = *req.Backend
	}
	if req.Model != nil {
		cand.Model = *req.Model
	}
	if req.SandboxOverride != nil {
		cand.SandboxOverride = *req.SandboxOverride
	}
	if req.Prompt != nil {
		cand.Prompt = *req.Prompt
	}
	if msg := s.validateRuntime(&cand, req); msg != "" {
		writeCodedErrCause(w, http.StatusBadRequest, msg, "invalid_runtime", nil)
		return
	}

	changed := changedRuntimeFields(task, &cand)
	if len(changed) == 0 {
		sess := s.liveRuntimeSession(id)
		s.writeRuntimeOK(w, task, req.Apply, changed, false, sess != nil)
		return
	}

	if req.Backend != nil && task.SessionID != "" && !sameFamily(task, &cand, cfg) {
		writeCodedErrCause(w, http.StatusConflict, "backend family cannot change while the task has a saved conversation", "conversation_incompatible", nil)
		return
	}
	if s.runner.HasPendingRestart(id) {
		writeCodedErrCause(w, http.StatusConflict, "agent is restarting", "agent_restarting", nil)
		return
	}
	sess := s.liveRuntimeSession(id)
	if req.Apply == applyNextResume && sess != nil {
		writeCodedErrCause(w, http.StatusConflict, "task has a live session; stop it or use apply=restart", "session_live", nil)
		return
	}
	if req.Apply == applyRestart && sess != nil && !sess.IsIdle() && !req.Force {
		writeCodedErrCause(w, http.StatusConflict, "agent is working; pass force to interrupt it", "task_busy", nil)
		return
	}
	if req.Apply == applyRestart {
		s.discoverRuntimeSession(&cand, cfg)
		if cand.SessionID != "" {
			if ok, err := agent.ResumableSessionAvailable(&cand, cfg); err != nil || !ok {
				uxlog.Log("[api] runtime: resume preflight failed task=%s ok=%t err=%v", id, ok, err)
				writeCodedErrCause(w, http.StatusConflict, "saved conversation cannot be resumed with these settings", "resume_unavailable", err)
				return
			}
		}
	}

	rollbackCloseout := func() {}
	if req.Apply == applyRestart && sess == nil {
		proceed, rb := s.closeoutGate(w, task, req.ForceRevive, "runtime")
		if !proceed {
			return
		}
		rollbackCloseout = rb
	}

	sandboxed := task.Sandboxed
	if req.SandboxOverride != nil || req.Backend != nil {
		sandboxed = agent.IsTaskSandboxed(&cand, cfg)
	}
	_, err = s.db.SetTaskRuntime(id, db.TaskRuntimeUpdate{
		Expected: task, SessionID: &cand.SessionID,
		Backend: cand.Backend, Model: cand.Model, SandboxOverride: cand.SandboxOverride,
		Sandboxed: sandboxed, Prompt: cand.Prompt,
	})
	if err != nil {
		rollbackCloseout()
		if errors.Is(err, db.ErrTaskRuntimeChanged) {
			uxlog.Log("[api] runtime: revision changed before commit task=%s", id)
			fresh := s.freshTask(id, task)
			writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": "runtime revision is stale", "code": "revision_stale", "revision": runtimeRevision(fresh)})
			return
		}
		if errors.Is(err, db.ErrTaskNotFound) {
			writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
			return
		}
		uxlog.Log("[api] runtime: persist failed task=%s err=%v", id, err)
		writeCodedErrCause(w, http.StatusInternalServerError, "", "store_failed", err)
		return
	}
	cand.Sandboxed = sandboxed
	uxlog.Log("[api] runtime: persisted task=%s apply=%s fields=%v", id, req.Apply, changed)

	if req.Apply == applyNextResume {
		s.emitRuntimeChanged(id, req.Apply, changed, false)
		s.writeRuntimeOK(w, s.freshTask(id, &cand), req.Apply, changed, false, false)
		return
	}

	if sess != nil {
		c, rows := sess.PTYSize()
		cols, rws := uint16(max(c, 0)), uint16(max(rows, 0)) //nolint:gosec // PTY sizes are small positive ints
		if cols == 0 || rws == 0 {
			rws, cols = 24, 80
		}
		restart := cand
		if err = s.runner.KickRerender(&restart, cfg, rws, cols); err != nil {
			uxlog.Log("[api] runtime: kick outcome unknown, keeping settings task=%s err=%v", id, err)
			s.emitRuntimeChanged(id, req.Apply, changed, false)
			s.writeRestartFailure(w, id, "restart_unknown", true, false, err)
			return
		}
		live, pending, probeErr := s.awaitRestartedSession(id)
		uxlog.Log("[api] runtime: kick settled task=%s live=%t pending=%t err=%v", id, live, pending, probeErr)
		if probeErr != nil || pending || !live {
			s.emitRuntimeChanged(id, req.Apply, changed, false)
			if probeErr != nil || pending {
				s.writeRestartFailure(w, id, "restart_unknown", true, false, probeErr)
			} else {
				s.writeRestartFailed(w, id, true, false, nil)
			}
			return
		}
		if err := s.db.SetStatus(id, model.StatusInProgress); err != nil {
			uxlog.Log("[api] runtime: restart status write failed task=%s err=%v", id, err)
			s.writeRestartFailure(w, id, "store_failed", true, false, err)
			return
		}
		s.emitRuntimeChanged(id, req.Apply, changed, true)
		s.writeRuntimeOK(w, s.freshTask(id, &cand), req.Apply, changed, true, live)
		return
	}

	if _, err := s.restartTaskSession(&cand); err != nil {
		if errors.Is(err, agent.ErrStartAmbiguous) {
			uxlog.Log("[api] runtime: start outcome unknown, skipping closeout rollback task=%s err=%v", id, err)
		} else {
			rollbackCloseout()
		}
		uxlog.Log("[api] runtime: start failed after persist task=%s err=%v", id, err)
		s.emitRuntimeChanged(id, req.Apply, changed, false)
		s.writeRestartFailed(w, id, true, false, err)
		return
	}
	s.emitRuntimeChanged(id, req.Apply, changed, true)
	s.writeRuntimeOK(w, s.freshTask(id, &cand), req.Apply, changed, true, s.liveRuntimeSession(id) != nil)
}

type runtimeRestartProber interface {
	ProbeRestart(taskID string) (live, pending bool, err error)
}

// awaitRestartedSession waits briefly for an in-place kick to bring the new
// session up (the runner relaunches after the old process exits) so the
// response reports the real session state rather than the mid-restart gap.
func (s *Server) awaitRestartedSession(id string) (live, pending bool, err error) {
	prober, ok := s.runner.(runtimeRestartProber)
	if !ok {
		return false, false, errors.New("runner cannot confirm restart state")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		live, pending, err = prober.ProbeRestart(id)
		if err != nil || !pending || time.Now().After(deadline) {
			return live, pending, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (s *Server) freshTask(id string, fallback *model.Task) *model.Task {
	if t, err := s.db.Get(id); err == nil && t != nil {
		return t
	}
	return fallback
}

func (s *Server) writeRuntimeOK(w http.ResponseWriter, t *model.Task, apply string, changed []string, started, live bool) {
	rev := runtimeRevision(t)
	w.Header().Set("ETag", `"`+rev+`"`)
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":        t.ID,
		"runtime":        runtimeOf(t),
		"apply":          apply,
		"changed_fields": changed,
		"revision":       rev,
		"session_id":     t.SessionID,
		"live":           live,
		"started":        started,
	})
}

// writeRestartFailed is the shared partial-outcome response for a controlled
// restart that did not come up: 500 {"error":"restart failed","code":
// "restart_failed","settings_applied","started":false,"live","revision"}.
// settings_applied is true when the new settings are stored (the caller must
// reconcile); false with rolled_back=true when the old session kept running
// and the stored settings were restored. Callers pass what actually happened.
func (s *Server) writeRestartFailed(w http.ResponseWriter, id string, settingsApplied, rolledBack bool, cause error, extra ...map[string]any) {
	s.writeRestartFailure(w, id, "restart_failed", settingsApplied, rolledBack, cause, extra...)
}

func (s *Server) writeRestartFailure(w http.ResponseWriter, id, code string, settingsApplied, rolledBack bool, cause error, extra ...map[string]any) {
	body := map[string]any{
		"error":            "restart failed",
		"code":             code,
		"settings_applied": settingsApplied,
		"started":          false,
		"live":             s.liveRuntimeSession(id) != nil,
		"rolled_back":      rolledBack,
	}
	if t, err := s.db.Get(id); err == nil && t != nil {
		body["revision"] = runtimeRevision(t)
	}
	if cause != nil {
		body["detail"] = cause.Error()
	}
	for _, e := range extra {
		for k, v := range e {
			body[k] = v
		}
	}
	writeJSON(w, http.StatusInternalServerError, body)
}

func (s *Server) emitRuntimeChanged(id, apply string, changed []string, started bool) {
	events.Emit(model.EventTypeTaskRuntimeChanged, id, map[string]any{
		"apply":          apply,
		"changed_fields": changed,
		"started":        started,
	})
}

func changedRuntimeFields(old, cur *model.Task) []string {
	out := []string{}
	if old.Model != cur.Model {
		out = append(out, "model")
	}
	if old.Backend != cur.Backend {
		out = append(out, "backend")
	}
	if old.SandboxOverride != cur.SandboxOverride {
		out = append(out, "sandbox_override")
	}
	if old.Prompt != cur.Prompt {
		out = append(out, "prompt")
	}
	return out
}

func sameFamily(old, cur *model.Task, cfg config.Config) bool {
	ob, err := agent.ResolveBackend(old, cfg)
	if err != nil {
		return false
	}
	nb, err := agent.ResolveBackend(cur, cfg)
	if err != nil {
		return false
	}
	return backendkind.Of(ob.Command) == backendkind.Of(nb.Command)
}

// validateRuntime checks every requested field and returns a 400 message, or
// "" when valid. Model validation is family-based: the family must accept a
// model at all and the id must be a plain model identifier.
func (s *Server) validateRuntime(cand *model.Task, req runtimePatchReq) string {
	cfg := s.db.Config()
	if req.Backend != nil {
		if strings.TrimSpace(*req.Backend) == "" {
			return "backend must name a configured backend"
		}
		if _, ok := cfg.Backends[*req.Backend]; !ok {
			return "backend " + *req.Backend + " is not configured"
		}
	}
	if req.SandboxOverride != nil {
		if err := model.ValidateSandboxOverride(*req.SandboxOverride); err != nil {
			return err.Error()
		}
	}
	if req.Prompt != nil && strings.ContainsRune(*req.Prompt, 0) {
		return "prompt must not contain NUL"
	}
	if req.Prompt != nil && strings.TrimSpace(*req.Prompt) == "" {
		return "prompt must not be blank"
	}
	if cand.Model != "" && (req.Model != nil || req.Backend != nil) {
		if len(cand.Model) > maxModelLen || !modelIDPattern.MatchString(cand.Model) {
			return "model is not a valid model id"
		}
		b, err := agent.ResolveBackend(cand, cfg)
		if err != nil {
			return err.Error()
		}
		if agent.FamilyOf(b.Command).ModelDelivery == agent.ModelNone {
			return "backend does not support model selection"
		}
	}
	return ""
}

func (s *Server) discoverRuntimeSession(task *model.Task, cfg config.Config) {
	if task.SessionID == "" || task.Worktree == "" {
		return
	}
	backend, err := agent.ResolveBackend(task, cfg)
	if err != nil {
		return
	}
	family := agent.FamilyOf(backend.Command)
	if !family.RecaptureEveryExit || family.Capture == nil {
		return
	}
	sid, err := family.Capture(task, cfg)
	if err != nil || sid == "" {
		uxlog.Log("[api] runtime: conversation discovery skipped task=%s err=%v", task.ID, err)
		return
	}
	if sid != task.SessionID {
		uxlog.Log("[api] runtime: discovered conversation task=%s session=%s", task.ID, sid)
		task.SessionID = sid
	}
}
