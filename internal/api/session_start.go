package api

import (
	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
)

func (s *Server) startTaskSession(task *model.Task, resume bool) (reattached bool, err error) {
	cfg := s.db.Config()
	// Resume-time recapture (Claude-only): target the newest worktree transcript
	// so the resume picks the most recent in-place session, not the stale
	// create-time UUID. No-op for non-Claude / no-change / no-transcript.
	if resume {
		agent.RefreshResumeSessionID(s.db, task)
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
