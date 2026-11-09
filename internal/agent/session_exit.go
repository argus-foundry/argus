package agent

import "context"

// SessionExitWaiter confirms exit independently of a remote stream lifetime.
type SessionExitWaiter interface {
	WaitForSessionExit(ctx context.Context, taskID string, expectedPID int) error
}

// SessionPresenceChecker detects sessions independently of cached stream state.
type SessionPresenceChecker interface {
	SessionPresent(ctx context.Context, taskID string) (bool, error)
}
