package client

import (
	"encoding/json"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// streamBackoff is the delay before each stream reconnect attempt after a drop
// (len = max retries). About 15s in total so a daemon restart (which takes
// seconds, not the ~1.5s the old 3x500ms budget allowed) is survived instead of
// leaving a live agent behind a dead handle. A var so tests can shrink it.
var streamBackoffPtr atomic.Pointer[[]time.Duration]

func init() {
	d := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	streamBackoffPtr.Store(&d)
}

// streamBackoff returns the current schedule. Atomic because a connectStream
// goroutine can outlive the test (or caller) that swapped the schedule.
func streamBackoff() []time.Duration { return *streamBackoffPtr.Load() }

// connectStream opens a stream connection to the daemon for this session's output.
// Reads raw bytes and writes them to the local ring buffer. If the stream drops
// but the daemon reports the session is still alive, retries up to maxStreamRetries
// times before giving up. Calls removeSession only when the process has actually
// exited. Calls removeSessionStreamLost when retries are exhausted, the daemon
// is unreachable, or the session was closed externally (e.g., client shutdown).
func (rs *RemoteSession) connectStream(sockPath string) {
	backoff := streamBackoff()
	maxStreamRetries := len(backoff)
	var restart daemon.ExitInfo
	rs.mu.Lock()
	launch := rs.info
	rs.mu.Unlock()
	waitingForLaunch := launch.Alive && launch.PID == 0
	for attempt := 0; attempt <= maxStreamRetries; attempt++ {
		// Check if the client was closed (e.g., daemon restart replaced it).
		// This stops stale goroutines from flooding the new daemon with
		// stream requests after a restart cascade. No removeSession* call
		// needed — Client.Close() iterates c.sessions to clean up.
		select {
		case <-rs.client.closed:
			uxlog.Log("stream: client closed, stopping retries task=%s", rs.taskID)
			rs.close()
			return
		case <-rs.done:
			uxlog.Log("stream: session closed externally, stopping retries task=%s", rs.taskID)
			rs.close()
			return
		default:
		}

		if attempt > 0 {
			uxlog.Log("stream: retry %d/%d task=%s", attempt, maxStreamRetries, rs.taskID)
			select {
			case <-time.After(backoff[attempt-1]):
			case <-rs.client.closed:
				rs.close()
				return
			case <-rs.done:
				return
			}
		}

		rs.streamAttempt = attempt
		if waitingForLaunch {
			if launch.RestartFromPID > 0 {
				exit := rs.client.exitInfoForPID(rs.taskID, launch.RestartFromPID)
				if exit.StreamLost || exit.PID != launch.RestartFromPID {
					continue
				}
				if !exit.PendingRestart {
					rs.client.finishSession(rs, exit)
					return
				}
				restart = exit
				waitingForLaunch = false
			}
		}
		if waitingForLaunch {
			info, err := rs.client.ProbeSession(rs.taskID)
			if err != nil {
				continue
			}
			if launch.LastExitPID > 0 {
				exit := rs.client.exitInfoForPID(rs.taskID, launch.LastExitPID)
				if exit.PendingRestart && !exit.StreamLost {
					restart = exit
					waitingForLaunch = false
				}
			}
			if waitingForLaunch {
				if info.Alive && info.PID == 0 {
					// The owner still has a reservation. Startup reattach
					// has no predecessor handle and must retain its relay.
					attempt = 0
					continue
				}
				if !info.Alive {
					pid := info.PID
					if pid == 0 {
						pid = info.LastExitPID
					}
					if pid == 0 || pid == launch.LastExitPID {
						rs.close()
						rs.client.removeSessionStreamLost(rs)
						return
					}
					exit := rs.client.exitInfoForPID(rs.taskID, pid)
					if exit.StreamLost || exit.PID != pid {
						// The owner may still have the retired handle
						// registered while its exit callback publishes.
						continue
					}
					if !exit.PendingRestart {
						rs.client.finishSession(rs, exit)
						return
					}
					restart = exit
				} else {
					rs.updateInfo(info)
				}
				waitingForLaunch = false
			}
		}
		if restart.PendingRestart {
			if !rs.client.ownsSession(rs) {
				rs.close()
				return
			}
			if restart.RestartSuccessorPID == 0 {
				exit := rs.client.exitInfoForPID(rs.taskID, restart.PID)
				if exit.StreamLost {
					continue
				}
				if !exit.PendingRestart {
					// A launch failure updates the outgoing process's exit.
					rs.client.finishSession(rs, exit)
					return
				}
				if exit.RestartSuccessorPID == 0 {
					if pending, err := rs.client.hasPendingRestart(rs.taskID); err == nil && pending {
						// Prelaunch at the owner can take minutes. A confirmed
						// pending restart gets the shortest polling delay,
						// leaving the retry budget for transport failures.
						attempt = 0
					}
					continue
				}
				restart = exit
				if !rs.client.relayRestartExit(rs, exit) {
					rs.close()
					return
				}
			}

			successorPID := restart.RestartSuccessorPID
			info, err := rs.client.ProbeSession(rs.taskID)
			var exit daemon.ExitInfo
			if err != nil || !info.Alive || info.PID != successorPID {
				// The successor may have exited before it could be attached.
				// Its scoped record also allows a chained restart to be followed
				// without treating a subsequent explicit launch as our successor.
				exit = rs.client.exitInfoForPID(rs.taskID, successorPID)
				if exit.StreamLost || exit.PID != successorPID {
					continue
				}
				info = daemon.SessionInfo{PID: successorPID}
			}
			if !rs.client.adoptRestart(rs, info) {
				rs.close()
				return
			}
			attempt = 0
			rs.streamAttempt = 0
			if exit.PID > 0 {
				if !exit.PendingRestart {
					rs.client.finishSession(rs, exit)
					return
				}
				if !rs.client.relayRestartExit(rs, exit) {
					rs.close()
					return
				}
				restart = exit
				continue
			}
			restart = daemon.ExitInfo{}
		}
		exited, daemonDown := rs.streamOnce(sockPath)
		if daemonDown {
			// Can't reach daemon — not a confirmed process exit. A restarting
			// daemon is back within seconds, so keep backing off (a closed
			// client/session stops the loop at the top) instead of dropping
			// the handle on the first failure.
			if attempt < maxStreamRetries {
				uxlog.Log("stream: daemon unreachable task=%s attempt=%d/%d, will retry", rs.taskID, attempt, maxStreamRetries)
				continue
			}
			uxlog.Log("stream: gave up, daemon unreachable after %d retries task=%s, treating as stream lost", maxStreamRetries, rs.taskID)
			rs.close()
			rs.client.removeSessionStreamLost(rs)
			return
		}
		if exited {
			// Process actually exited — fire the exit callback.
			exit, pendingRestart := rs.client.removeSession(rs)
			if !pendingRestart {
				return
			}
			restart = exit
			attempt = 0
			continue
		}

		// Stream dropped but process is still alive — check if we
		// should retry or if we've been closed externally.
		select {
		case <-rs.done:
			// Session was closed externally (e.g., client shutdown during
			// daemon restart) — we don't know if the process actually
			// exited, so treat as stream lost to avoid incorrectly
			// marking the task Complete.
			uxlog.Log("stream: session closed externally task=%s, treating as stream lost", rs.taskID)
			rs.close() // no-op (done already closed), but consistent with other exit paths
			rs.client.removeSessionStreamLost(rs)
			return
		default:
		}
	}

	// Exhausted retries — stream lost, not a confirmed process exit.
	uxlog.Log("stream: gave up after %d retries task=%s, treating as stream lost", maxStreamRetries, rs.taskID)
	rs.close()
	rs.client.removeSessionStreamLost(rs)
}

func (c *Client) ownsSession(rs *RemoteSession) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[rs.taskID] == rs
}

// adoptRestart changes the process behind a handle only after the outgoing
// owner confirmed an in-place restart. Explicit starts install their own handle.
func (c *Client) adoptRestart(rs *RemoteSession, info daemon.SessionInfo) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[rs.taskID] != rs {
		return false
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.Alive() {
		return false
	}
	uxlog.Log("stream: following managed restart task=%s old_pid=%d pid=%d", rs.taskID, rs.pid, info.PID)
	for w := range rs.pidBoundWriters {
		rs.removeWriterLocked(w)
	}
	if rs.processDone != nil {
		close(rs.processDone)
		rs.processDone = nil
	}
	rs.pid, rs.info = info.PID, info
	rs.streamBase = rs.buf.TotalWritten()
	rs.buf.Reset()
	rs.composerTyped = false
	rs.composerGeneration++
	return true
}

// streamOnce opens a single stream connection and reads until EOF or error.
// Returns (processExited, daemonDown):
//   - (true, false)  — process has exited, fire exit callback
//   - (false, false) — stream dropped but process still alive, should retry
//   - (false, true)  — daemon is unreachable, treat as stream lost
func (rs *RemoteSession) streamOnce(sockPath string) (processExited, daemonDown bool) {
	// Early exit if closed (e.g., client shutdown during daemon restart).
	// Avoids dialing the new daemon's socket with stale session IDs.
	select {
	case <-rs.done:
		return false, true
	default:
	}
	uxlog.Log("stream: connecting task=%s", rs.taskID)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		uxlog.Log("stream: DIAL FAILED task=%s err=%v", rs.taskID, err)
		alive, reachable := rs.isSessionAlive()
		if !reachable {
			return false, true
		}
		return !alive, false
	}
	defer conn.Close()

	// Send stream prefix byte.
	if _, err := conn.Write([]byte("S")); err != nil {
		uxlog.Log("stream: WRITE PREFIX FAILED task=%s err=%v", rs.taskID, err)
		alive, reachable := rs.isSessionAlive()
		if !reachable {
			return false, true
		}
		return !alive, false
	}

	// Send stream header to subscribe to this session's output.
	// Pass TotalWritten() so the daemon replays only the missed delta on
	// reconnects — on the first attach this is 0 (full replay). On retries
	// (network blip, daemon kick-restart gap, retry budget) it skips bytes
	// already written to rs.buf, preventing duplicate content from being
	// re-emulated by x/vt (e.g., a stale status bar bake-in).
	enc := json.NewEncoder(conn)
	rs.mu.Lock()
	pid := rs.pid
	since := rs.buf.TotalWritten() - rs.streamBase
	rs.mu.Unlock()
	if err := enc.Encode(daemon.StreamHeader{
		TaskID:      rs.taskID,
		Since:       since,
		ExpectedPID: pid,
	}); err != nil {
		uxlog.Log("stream: ENCODE HEADER FAILED task=%s err=%v", rs.taskID, err)
		alive, reachable := rs.isSessionAlive()
		if !reachable {
			return false, true
		}
		return !alive, false
	}

	uxlog.Log("stream: connected task=%s", rs.taskID)
	if rs.streamAttempt > 0 {
		rs.refreshInfo()
		uxlog.Log("stream: reconnected task=%s after %d retries", rs.taskID, rs.streamAttempt)
	}

	// Read output stream into local ring buffer, teeing to any attached writers.
	// The tee mirrors agent.Session.readLoop: snapshot the writer slice under
	// the lock, write outside it (a TUI net.Conn writer may block on flow
	// control — holding rs.mu through the write would stall this reader and back
	// up the whole double-proxy), then drop errored writers under the lock. This
	// is the supervisor→daemon→TUI fan-out hop; in plain daemon mode no writers
	// are attached so it's just the buf.Write.
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data := buf[:n]
			rs.mu.Lock()
			rs.buf.Write(data)
			var ws []io.Writer
			if len(rs.writers) > 0 {
				ws = make([]io.Writer, len(rs.writers))
				copy(ws, rs.writers)
			}
			rs.mu.Unlock()

			var failed []io.Writer
			for _, w := range ws {
				if _, werr := w.Write(data); werr != nil {
					failed = append(failed, w)
				}
			}
			if len(failed) > 0 {
				rs.mu.Lock()
				for _, f := range failed {
					rs.removeWriterLocked(f)
				}
				rs.mu.Unlock()
			}
		}
		if err != nil {
			uxlog.Log("stream: ended task=%s err=%v", rs.taskID, err)
			break
		}
	}

	// Stream ended — check if the process actually exited.
	alive, reachable := rs.isSessionAlive()
	if !reachable {
		return false, true
	}
	return !alive, false
}

// isSessionAlive checks with the daemon whether the session's process is
// still running. Returns (alive, daemonReachable). If the RPC fails, daemon
// may be down — returns (false, false).
func (rs *RemoteSession) isSessionAlive() (alive bool, daemonReachable bool) {
	var info daemon.SessionInfo
	if err := rs.client.call("Daemon.SessionStatus", &daemon.TaskIDReq{TaskID: rs.taskID}, &info); err != nil {
		uxlog.Log("stream: SessionStatus RPC failed task=%s err=%v (daemon unreachable)", rs.taskID, err)
		return false, false
	}
	uxlog.Log("stream: SessionStatus task=%s alive=%v pid=%d", rs.taskID, info.Alive, info.PID)
	if pid := rs.PID(); pid > 0 && ((info.PID > 0 && info.PID != pid) || info.LastExitPID == pid) {
		return false, true
	}
	return info.Alive, true
}
