package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// moveAccountRequest is the POST /api/tasks/{id}/account body. ForceRevive
// is only consulted for a task that awaits Hera close-out and has no live
// session.
type moveAccountRequest struct {
	Account     string `json:"account"`
	Force       bool   `json:"force"`
	ForceRevive bool   `json:"force_revive"`
	// Backend, when set, moves the task to another agent CLI by a restart with
	// handoff (the conversation is not carried over). Account then defaults to
	// "default" and must support that backend.
	Backend string `json:"backend"`
}

// moveOutcome describes how a move settled beyond the HTTP status.
type moveOutcome struct {
	// persisted: the stored account changed, so the restart failure is partial.
	persisted bool
	unknown   bool
}

// handleMoveTaskAccount moves a task to another account and resumes its
// conversation there (agent.MoveTaskAccount). With `backend` it instead moves
// the task to another CLI (agent.MoveTaskBackend): a restart with handoff, not
// a resume. A live session is restarted in place; an exited one goes through
// the same path as /restart. A restart that fails after the account changed
// reports the partial outcome.
func (s *Server) handleMoveTaskAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req moveAccountRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body", err)
		return
	}
	unlock, err := s.lockTaskMutation(r.Context(), id)
	if err != nil {
		uxlog.Log("[account] move task=%s lock canceled: %v", id, err)
		writeErr(w, http.StatusServiceUnavailable, "task operation canceled", err)
		return
	}
	defer unlock()
	if err := r.Context().Err(); err != nil {
		uxlog.Log("[account] move task=%s canceled after lock: %v", id, err)
		writeErr(w, http.StatusServiceUnavailable, "task operation canceled", err)
		return
	}

	rollbackCloseout := func() {}
	if task, err := s.db.Get(id); err == nil && task != nil && s.liveRuntimeSession(id) == nil {
		proceed, rb := s.closeoutGate(w, task, req.ForceRevive, "account")
		if !proceed {
			return
		}
		rollbackCloseout = rb
	}

	res, status, out, err := s.moveTaskAccountLocked(agent.AccountMoveInput{TaskID: id, Account: req.Account, Backend: req.Backend, Force: req.Force})
	if err != nil {
		if !errors.Is(err, agent.ErrStartAmbiguous) && !out.unknown {
			rollbackCloseout()
		} else {
			uxlog.Log("[account] move task=%s outcome unknown; skipping closeout rollback: %v", id, err)
		}
		switch {
		case out.unknown:
			s.writeRestartFailure(w, id, "restart_unknown", true, false, err, map[string]any{"account": res.To, "moved": true})
		case out.persisted:
			s.writeRestartFailed(w, id, true, false, err, map[string]any{"account": res.To, "moved": true})
		default:
			writeErr(w, status, err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// MoveTaskAccount is the move without the HTTP status, for the MCP
// task_move_account tool. It takes the per-task lock itself.
func (s *Server) MoveTaskAccount(in agent.AccountMoveInput) (agent.AccountMoveResult, error) {
	defer s.taskLocks.Lock(in.TaskID)()
	res, _, _, err := s.moveTaskAccountLocked(in)
	return res, err
}

// moveTaskAccountLocked runs the move plus the restart fallback and maps
// failures to an HTTP status; the caller holds the task lock. It emits
// task.account_changed once the account is persisted, whatever the restart did.
func (s *Server) moveTaskAccountLocked(in agent.AccountMoveInput) (agent.AccountMoveResult, int, moveOutcome, error) {
	task, res, err := agent.MoveTaskAccount(s.db, s.runner, in)
	switch {
	case errors.Is(err, db.ErrTaskNotFound):
		return res, http.StatusNotFound, moveOutcome{}, err
	case errors.Is(err, agent.ErrAccountMoveBusy), errors.Is(err, db.ErrGraduationPending):
		return res, http.StatusConflict, moveOutcome{}, err
	case errors.Is(err, agent.ErrAccountMoveRestartUnknown):
		s.emitAccountChanged(in.TaskID, in, res)
		return res, http.StatusInternalServerError, moveOutcome{persisted: true, unknown: true}, err
	case errors.Is(err, agent.ErrAccount):
		return res, http.StatusBadRequest, moveOutcome{}, err
	case err != nil:
		return res, http.StatusInternalServerError, moveOutcome{}, err
	}
	if res.Started {
		live, pending, probeErr := s.awaitRestartedSession(task.ID)
		uxlog.Log("[account] move restart probe task=%s live=%t pending=%t err=%v", task.ID, live, pending, probeErr)
		if probeErr != nil || pending || !live {
			res.Started = false
			s.emitAccountChanged(task.ID, in, res)
			cause := probeErr
			if cause == nil {
				cause = fmt.Errorf("replacement not confirmed live (live=%t pending=%t)", live, pending)
			}
			return res, http.StatusInternalServerError, moveOutcome{persisted: true, unknown: probeErr != nil || pending}, fmt.Errorf("account moved to %q but restart unconfirmed: %w", res.To, cause)
		}
	} else {
		if _, err := s.startTaskSession(task, res.Resumed || in.Backend != ""); err != nil {
			uxlog.Log("[account] move task=%s: restart on %s failed: %v", task.ID, res.To, err)
			s.emitAccountChanged(task.ID, in, res)
			return res, http.StatusInternalServerError, moveOutcome{persisted: true}, fmt.Errorf("account moved to %q but restart failed: %w", res.To, err)
		}
		res.Started = true
	}
	s.emitAccountChanged(task.ID, in, res)
	return res, http.StatusOK, moveOutcome{}, nil
}

func (s *Server) emitAccountChanged(id string, in agent.AccountMoveInput, res agent.AccountMoveResult) {
	if in.Backend != "" {
		return // a backend move is not an account change
	}
	uxlog.Log("[account] move task=%s %s→%s resumed=%t started=%t settled", id, res.From, res.To, res.Resumed, res.Started)
	events.Emit(model.EventTypeTaskAccountChanged, id, map[string]any{
		"from": res.From, "to": res.To, "resumed": res.Resumed, "started": res.Started,
	})
}
