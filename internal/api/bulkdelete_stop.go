package api

import (
	"context"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
)

func (s *Server) waitDeleteSessionExit(id string, handle agent.SessionHandle) error {
	ctx, cancel := context.WithTimeout(s.deleteContext(), 10*time.Second)
	defer cancel()
	// Supervisor stream loss alone does not confirm process exit.
	if remote, ok := s.runner.(agent.SessionExitWaiter); ok {
		pid := 0
		if handle != nil {
			pid = handle.PID()
		}
		return remote.WaitForSessionExit(ctx, id, pid)
	}
	if handle != nil && handle.Alive() {
		select {
		case <-handle.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
		if handle.Alive() {
			return fmt.Errorf("session still alive")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if current := s.runner.Get(id); current != nil && current.Alive() {
		return fmt.Errorf("session replaced during stop")
	}
	if s.runner.HasPendingRestart(id) {
		return fmt.Errorf("session restart pending")
	}
	return nil
}

func (s *Server) deleteSessionPresent(id string) (bool, error) {
	ctx, cancel := context.WithTimeout(s.deleteContext(), 10*time.Second)
	defer cancel()
	if remote, ok := s.runner.(agent.SessionPresenceChecker); ok {
		return remote.SessionPresent(ctx, id)
	}
	if s.runner.HasPendingRestart(id) {
		return true, nil
	}
	if handle := s.runner.Get(id); handle != nil {
		return handle.Alive(), nil
	}
	return s.runner.HasSession(id), nil
}
