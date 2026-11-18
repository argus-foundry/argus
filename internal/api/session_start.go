package api

import (
	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
)

func (s *Server) startTaskSession(task *model.Task, resume bool) (reattached bool, err error) {
	return s.startTaskSessionFill(task, resume, agent.RefreshFillMissing)
}

// startTaskSessionFill is startTaskSession with an explicit empty-ID policy; a backend move keeps it empty.
func (s *Server) startTaskSessionFill(task *model.Task, resume bool, mode agent.SessionRefresh) (reattached bool, err error) {
	cfg := s.db.Config()
	if resume {
		agent.RefreshSessionIDWith(s.db, task, mode)
	}
	_ = agent.RestoreAcceptedCoordinatorHat(s.db, task.ID) // revived accepted sub-coordinator
	sess, reattached, err := s.runner.StartOrReattach(task, cfg, 24, 80, resume)
	if err != nil {
		return false, err
	}
	task.SetStatus(model.StatusInProgress)
	task.AgentPID = sess.PID()
	s.db.Update(task) //nolint:errcheck
	return reattached, nil
}
