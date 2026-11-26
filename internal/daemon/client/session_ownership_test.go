package client

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestStartStreamOwnership(t *testing.T) {
	c := &Client{
		sockPath: filepath.Join(t.TempDir(), "missing.sock"),
		sessions: make(map[string]*RemoteSession),
		closed:   make(chan struct{}),
	}
	t.Cleanup(func() { _ = c.Close() })

	old := newRemoteSession("task", c)
	t.Cleanup(old.close)
	old.updateInfo(daemon.SessionInfo{PID: 111})
	old.buf.Write([]byte("old output"))
	c.sessions["task"] = old

	newSession := c.sessionForStart("task", 222)
	testutil.True(t, newSession != old)
	testutil.True(t, newSession == c.sessionForStart("task", 222))
	testutil.Equal(t, newSession.PID(), 222)
	testutil.Equal(t, newSession.TotalWritten(), uint64(0))
	testutil.Equal(t, old.PID(), 111)
	testutil.Equal(t, string(old.RecentOutput()), "old output")

	var callbackOwner agent.SessionHandle
	var callbackPID int
	c.OnSessionExitOwned(func(_ string, info daemon.ExitInfo, owner agent.SessionHandle) {
		callbackOwner, callbackPID = owner, info.PID
	})
	c.removeSessionStreamLost(old)
	testutil.True(t, callbackOwner == old)
	testutil.Equal(t, callbackPID, 111)
	testutil.True(t, c.Get("task") == newSession)
}

func TestManagedRestartStreamPosition(t *testing.T) {
	c := &Client{sessions: make(map[string]*RemoteSession)}
	rs := newRemoteSession("task", c)
	t.Cleanup(rs.close)
	rs.updateInfo(daemon.SessionInfo{PID: 111})
	rs.buf.Write([]byte("old output"))
	c.sessions["task"] = rs
	oldDone := rs.ProcessDone(111)
	var oldWriter bytes.Buffer
	testutil.True(t, rs.AddWriterFromPID(&oldWriter, 0, 111))

	testutil.True(t, c.adoptRestart(rs, daemon.SessionInfo{PID: 222, Alive: true}))
	select {
	case <-oldDone:
	default:
		t.Fatal("managed restart left the old process stream open")
	}
	testutil.Equal(t, len(rs.writers), 0)
	testutil.Equal(t, string(rs.RecentOutput()), "")
	rs.buf.Write([]byte("new output"))

	var stale, current bytes.Buffer
	testutil.False(t, rs.AddWriterFromPID(&stale, 0, 111))
	testutil.Equal(t, stale.String(), "")
	testutil.True(t, rs.AddWriterFromPID(&current, 4, 222))
	testutil.Equal(t, current.String(), "output")
}

type replayLockWriter struct {
	session *RemoteSession
	bytes.Buffer
	replayedUnderLock bool
}

func (w *replayLockWriter) Write(p []byte) (int, error) {
	if w.session.mu.TryLock() {
		w.session.mu.Unlock()
	} else {
		w.replayedUnderLock = true
	}
	return w.Buffer.Write(p)
}

func TestProcessSSEReplayJoinsLiveOutputUnderLock(t *testing.T) {
	rs := newRemoteSession("task", &Client{})
	t.Cleanup(rs.close)
	rs.updateInfo(daemon.SessionInfo{PID: 111})
	rs.buf.Write([]byte("history"))
	sse := &replayLockWriter{session: rs}
	socket := &replayLockWriter{session: rs}

	testutil.True(t, rs.AddWriterFromPID(sse, 3, 111))
	testutil.True(t, sse.replayedUnderLock)
	testutil.Equal(t, sse.String(), "tory")
	testutil.True(t, rs.AddWriterFromPIDTolerant(socket, 3, 111))
	testutil.False(t, socket.replayedUnderLock)
	testutil.Equal(t, socket.String(), "tory")
	testutil.Equal(t, len(rs.writers), 2)
}
