package agent

import "errors"

var (
	ErrAlreadyAttached = errors.New("session is already attached")
	ErrNotRunning      = errors.New("process is not running")
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionStarting = errors.New("session is starting or restarting")
	// ErrSessionCoordinationUnsupported means an older supervisor cannot
	// distinguish a settled session from one in transition.
	ErrSessionCoordinationUnsupported = errors.New("the session supervisor does not support coordinated session operations. Use Settings > System > Restart Session Supervisor, then try again")
)
