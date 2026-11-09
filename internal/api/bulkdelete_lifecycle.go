package api

import (
	"context"
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

func (s *Server) deleteContext() context.Context {
	s.deleteJobMu.Lock()
	defer s.deleteJobMu.Unlock()
	if s.deleteJobCtx == nil {
		s.deleteJobCtx, s.deleteJobCancel = context.WithCancel(context.Background()) //nolint:gosec // G118: stopDeleteJobs invokes the stored cancel.
	}
	return s.deleteJobCtx
}

func (s *Server) stopDeleteJobs(ctx context.Context) error {
	s.deleteContext()
	s.deleteJobMu.Lock()
	s.deleteJobsStopping = true
	s.deleteJobCancel()
	s.deleteJobMu.Unlock()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { s.deleteJobWG.Wait(); close(done) }()
	select {
	case <-done:
		uxlog.Log("[api] bulk-delete: shutdown drained")
		return nil
	case <-waitCtx.Done():
		uxlog.Log("[api] bulk-delete: shutdown wait ended: %v", waitCtx.Err())
		return waitCtx.Err()
	}
}
