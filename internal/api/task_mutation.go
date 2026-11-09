package api

import "context"

// lockTaskMutation shares the REST task lock with archive and input.
func (s *Server) lockTaskMutation(ctx context.Context, id string) (func(), error) {
	return s.taskLocks.LockContext(ctx, id)
}
