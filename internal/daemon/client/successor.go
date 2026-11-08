package client

import (
	"fmt"
	"time"
)

// ConnectLive dials sockPath and requires a successful Ping on that fresh
// connection. Unlike Connect (whose eager Ping is best-effort, so it can hand
// back a client to a half-dead listener), a nil error here means a daemon is
// really answering. Used to tell "my connection died" from "the daemon died".
func ConnectLive(sockPath string) (*Client, error) {
	c, err := Connect(sockPath)
	if err != nil {
		return nil, err
	}
	if err := c.Ping(); err != nil {
		c.Close() //nolint:errcheck // discarding a client that failed its liveness probe
		return nil, fmt.Errorf("daemon at %s not responding: %w", sockPath, err)
	}
	return c, nil
}

// liveBootedAt returns the BootedAt of whichever daemon currently answers on
// sockPath, or the zero time if none does. It identifies the daemon process
// generation without a wire change (BootInfo already carries BootedAt).
func liveBootedAt(sockPath string) time.Time {
	c, err := ConnectLive(sockPath)
	if err != nil {
		return time.Time{}
	}
	defer c.Close() //nolint:errcheck // short-lived probe client
	info, err := c.BootInfo()
	if err != nil {
		return time.Time{}
	}
	return info.BootedAt
}

// awaitSuccessor polls sockPath until a live daemon answers whose BootedAt
// differs from prev, i.e. one that is NOT the daemon that owned the socket
// before the spawn. This is the fix for the endless restart loop: a freshly
// spawned daemon kills its predecessor ~1-2s after starting, so the first
// successful connect during that window reaches the doomed OLD daemon;
// returning that client leaves the caller holding a connection about to die.
// A zero prev means no daemon was answering beforehand, so the first live
// daemon is accepted.
func awaitSuccessor(sockPath string, prev time.Time, interval, maxWait time.Duration) (*Client, error) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		c, err := ConnectLive(sockPath)
		if err != nil {
			continue
		}
		info, err := c.BootInfo()
		if err == nil && (prev.IsZero() || !info.BootedAt.Equal(prev)) {
			return c, nil
		}
		c.Close() //nolint:errcheck // old/unidentifiable daemon: drop and keep waiting
	}
	return nil, fmt.Errorf("replacement daemon did not become ready within %s", maxWait)
}
