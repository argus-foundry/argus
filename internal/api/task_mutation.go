package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// lockTaskMutation shares the REST task lock with archive and input.
func (s *Server) lockTaskMutation(ctx context.Context, id string) (func(), error) {
	return s.taskLocks.LockContext(ctx, id)
}

// Check untrusted IDs before opening a permanent lock file. Callers read the
// task again under the lock before making decisions about its lifecycle.
func (s *Server) tryLockExistingSessionMutation(id string) (func(), error) {
	if _, err := s.db.Get(id); err != nil {
		return nil, err
	}
	unlock, acquired, err := s.db.TryLockSessionLaunch(id)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, agent.ErrSessionStarting
	}
	return unlock, nil
}

// lockSessionMutationHTTP must be called after lockTaskMutation when a handler
// uses both locks. Native TUI session operations share only the DB lock.
func (s *Server) lockSessionMutationHTTP(w http.ResponseWriter, id string) (func(), bool) {
	unlock, err := s.tryLockExistingSessionMutation(id)
	switch {
	case errors.Is(err, db.ErrTaskNotFound):
		writeCodedErrCause(w, http.StatusNotFound, "task not found", "task_not_found", nil)
	case errors.Is(err, agent.ErrSessionStarting):
		writeSessionMutationConflict(w)
	case errors.Is(err, db.ErrHeraReclaimInProgress):
		writeCodedErrCause(w, http.StatusConflict, "", "reclaim_pending", err)
	case err != nil:
		uxlog.Log("[api] session operation lock failed task=%s err=%v", id, err)
		writeCodedErrCause(w, http.StatusInternalServerError, "could not coordinate session operation", "session_lock_failed", nil)
	default:
		return unlock, true
	}
	return nil, false
}

func writeSessionMutationConflict(w http.ResponseWriter) {
	writeCodedErrCause(w, http.StatusConflict, "agent is starting or restarting", "agent_restarting", nil)
}
