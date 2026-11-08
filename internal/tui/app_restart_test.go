package tui

import (
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/daemon"
	dclient "github.com/drn/argus/internal/daemon/client"
	"github.com/drn/argus/internal/testutil"
)

// fakeShutdownDaemon is a minimal unix-socket RPC server exposing just enough
// of the "Daemon" RPC surface (Ping + Shutdown) to exercise
// gracefulDaemonShutdown without forking a real daemon process. Mirrors the
// wire protocol dclient.Connect expects: a leading 'R' byte, then a JSON-RPC
// codec (see internal/daemon/client/fakedaemon_test.go for the sibling used
// by that package's own tests — duplicated narrowly here since its type is
// unexported and this package cannot import it).
type fakeShutdownDaemon struct {
	sock          string
	ln            net.Listener
	shutdownCalls atomic.Int32
	failShutdown  bool
}

func newFakeShutdownDaemon(t *testing.T, failShutdown bool) *fakeShutdownDaemon {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "f.sock")
	ln, err := net.Listen("unix", sock)
	testutil.NoError(t, err)
	fd := &fakeShutdownDaemon{sock: sock, ln: ln, failShutdown: failShutdown}
	t.Cleanup(func() { ln.Close() }) //nolint:errcheck // best-effort cleanup
	go fd.serve()
	return fd
}

type fakeShutdownRPC struct{ fd *fakeShutdownDaemon }

func (s *fakeShutdownRPC) Ping(_ *daemon.Empty, resp *daemon.PongResp) error {
	resp.OK = true
	return nil
}

func (s *fakeShutdownRPC) Shutdown(_ *daemon.Empty, resp *daemon.StatusResp) error {
	s.fd.shutdownCalls.Add(1)
	if s.fd.failShutdown {
		resp.Error = "fake-shutdown-fail"
	}
	return nil
}

func (fd *fakeShutdownDaemon) serve() {
	server := rpc.NewServer()
	server.RegisterName("Daemon", &fakeShutdownRPC{fd: fd}) //nolint:errcheck
	for {
		conn, err := fd.ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close() //nolint:errcheck // best-effort close on a finished conn
			prefix := make([]byte, 1)
			if _, err := c.Read(prefix); err != nil {
				return
			}
			if prefix[0] == 'R' {
				server.ServeCodec(jsonrpc.NewServerCodec(c))
			}
		}(conn)
	}
}

// TestGracefulShutdown_Calls pins the fix: a daemon bounce must ask the
// connected daemon to exit via the Shutdown RPC, not merely close the local
// connection (which leaves the remote process alive — see the
// fix-daemon-restart-shutdown-rpc openspec change for the failure mode this
// closes). Name kept short: macOS 104-byte unix socket path limit
// (t.TempDir() embeds the test name in the socket path).
func TestGracefulShutdown_Calls(t *testing.T) {
	fd := newFakeShutdownDaemon(t, false)
	c, err := dclient.Connect(fd.sock)
	testutil.NoError(t, err)

	gracefulDaemonShutdown(c, fd.sock)

	testutil.Equal(t, fd.shutdownCalls.Load(), int32(1))
}

// TestGracefulShutdown_Fails pins that a Shutdown RPC failure (e.g.
// the connection is already dead) does not prevent the local connection from
// being closed — the bounce must proceed regardless.
func TestGracefulShutdown_Fails(t *testing.T) {
	fd := newFakeShutdownDaemon(t, true)
	c, err := dclient.Connect(fd.sock)
	testutil.NoError(t, err)

	// Must not panic or hang even though the daemon reports a Shutdown error.
	gracefulDaemonShutdown(c, fd.sock)

	// Own connection + the fresh-connection retry.
	testutil.Equal(t, fd.shutdownCalls.Load(), int32(2))
}

// TestGracefulShutdown_FreshConn pins the loop fix's shutdown half: when the
// TUI's own connection is dead (Shutdown over it fails) but a live daemon
// still owns the socket, the daemon must still be told to exit via a fresh
// connection — otherwise AutoStart connects to it and the loop re-arms.
func TestGracefulShutdown_FreshConn(t *testing.T) {
	fd := newFakeShutdownDaemon(t, false)
	c, err := dclient.Connect(fd.sock)
	testutil.NoError(t, err)
	c.Close() //nolint:errcheck // simulate our connection dying

	gracefulDaemonShutdown(c, fd.sock)

	testutil.Equal(t, fd.shutdownCalls.Load(), int32(1))
}

// TestReconnectLive_Adopts: a live daemon answers on a fresh connection, so
// the App swaps clients, resets the failure streak, and reports true (the
// caller then spawns nothing).
func TestReconnectLive_Adopts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fd := newFakeShutdownDaemon(t, false)
	app := New(testDB(t), agent.NewRunner(nil), true)
	app.sockPathFn = func() string { return fd.sock }
	app.daemonFailures = 5
	_, stop := wireApp(t, app)
	defer stop()

	if !app.reconnectLiveDaemon() {
		t.Fatal("reconnectLiveDaemon = false, want true with a live daemon")
	}
	syncUI(t, app.tapp)
	app.mu.Lock()
	defer app.mu.Unlock()
	testutil.Equal(t, app.daemonFailures, 0)
	if app.daemonClient == nil || app.restartedClient == nil {
		t.Fatal("daemonClient not swapped to the fresh connection")
	}
	testutil.Equal(t, fd.shutdownCalls.Load(), int32(0))
}

// TestReconnectLive_NoDaemon: nothing answers → false, state untouched.
func TestReconnectLive_NoDaemon(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := New(testDB(t), agent.NewRunner(nil), true)
	app.sockPathFn = func() string { return filepath.Join(t.TempDir(), "n.sock") }
	app.daemonFailures = 4
	if app.reconnectLiveDaemon() {
		t.Fatal("reconnectLiveDaemon = true with no daemon")
	}
	testutil.Equal(t, app.daemonFailures, 4)
}
