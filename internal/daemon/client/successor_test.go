package client

import (
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

// bootFake serves Ping + BootInfo (+ optional Ping failure) on a unix socket,
// identifying itself by bootedAt. Names are short: macOS 104-byte socket limit.
type bootFake struct {
	ln       net.Listener
	bootedAt time.Time
	pingFail bool
}

type bootRPC struct{ f *bootFake }

func (s *bootRPC) Ping(_ *daemon.Empty, resp *daemon.PongResp) error {
	if s.f.pingFail {
		return os.ErrInvalid
	}
	resp.OK = true
	return nil
}

func (s *bootRPC) BootInfo(_ *daemon.Empty, resp *daemon.BootInfoResp) error {
	resp.BootedAt = s.f.bootedAt
	return nil
}

func startBootFake(t *testing.T, sock string, bootedAt time.Time, pingFail bool) *bootFake {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	testutil.NoError(t, err)
	f := &bootFake{ln: ln, bootedAt: bootedAt, pingFail: pingFail}
	srv := rpc.NewServer()
	srv.RegisterName("Daemon", &bootRPC{f: f}) //nolint:errcheck
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close() //nolint:errcheck
				p := make([]byte, 1)
				if _, err := c.Read(p); err != nil || p[0] != 'R' {
					return
				}
				srv.ServeCodec(jsonrpc.NewServerCodec(c))
			}(c)
		}
	}()
	t.Cleanup(func() { ln.Close() }) //nolint:errcheck
	return f
}

// shortSock returns a socket path in a short temp dir (macOS 104-byte limit;
// t.TempDir embeds the long test name).
func shortSock(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "ac")
	testutil.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(d) }) //nolint:errcheck
	return filepath.Join(d, "s.sock")
}

func TestConnectLive(t *testing.T) {
	t.Run("live", func(t *testing.T) {
		sock := shortSock(t)
		startBootFake(t, sock, time.Now(), false)
		c, err := ConnectLive(sock)
		testutil.NoError(t, err)
		c.Close() //nolint:errcheck
	})
	t.Run("no socket", func(t *testing.T) {
		if _, err := ConnectLive(shortSock(t)); err == nil {
			t.Fatal("want error for missing socket")
		}
	})
	t.Run("listener that fails ping", func(t *testing.T) {
		sock := shortSock(t)
		startBootFake(t, sock, time.Now(), true)
		if _, err := ConnectLive(sock); err == nil {
			t.Fatal("Connect swallows Ping errors; ConnectLive must not")
		}
	})
}

// TestAwaitSuccessor_OldOwnsSocket reproduces the restart loop: when the new
// daemon is spawned the OLD daemon still owns the socket (and answers) until
// the successor kills it. awaitSuccessor must not return a client to the old
// daemon; it must keep waiting and return the replacement.
func TestAwaitSuccessor_OldOwnsSocket(t *testing.T) {
	sock := shortSock(t)
	oldBoot := time.Now().Add(-time.Hour).Truncate(time.Second)
	newBoot := time.Now().Truncate(time.Second)
	old := startBootFake(t, sock, oldBoot, false)

	prev := liveBootedAt(sock)
	testutil.Equal(t, prev.Equal(oldBoot), true)

	// Successor takes over the socket after the old daemon is killed.
	go func() {
		time.Sleep(300 * time.Millisecond)
		old.ln.Close()  //nolint:errcheck
		os.Remove(sock) //nolint:errcheck
		startBootFake(t, sock, newBoot, false)
	}()

	c, err := awaitSuccessor(sock, prev, 10*time.Millisecond, 5*time.Second)
	testutil.NoError(t, err)
	defer c.Close() //nolint:errcheck
	info, err := c.BootInfo()
	testutil.NoError(t, err)
	testutil.Equal(t, info.BootedAt.Equal(newBoot), true)
}

func TestAwaitSuccessor_TimesOutOnOldDaemon(t *testing.T) {
	sock := shortSock(t)
	boot := time.Now().Truncate(time.Second)
	startBootFake(t, sock, boot, false)
	if _, err := awaitSuccessor(sock, boot, 10*time.Millisecond, 200*time.Millisecond); err == nil {
		t.Fatal("must not accept the daemon that predated the spawn")
	}
}

func TestAwaitSuccessor_NoPredecessor(t *testing.T) {
	sock := shortSock(t)
	startBootFake(t, sock, time.Now(), false)
	c, err := awaitSuccessor(sock, time.Time{}, 10*time.Millisecond, 2*time.Second)
	testutil.NoError(t, err)
	c.Close() //nolint:errcheck
}

func TestLiveBootedAt_NoDaemon(t *testing.T) {
	if got := liveBootedAt(shortSock(t)); !got.IsZero() {
		t.Fatalf("want zero, got %v", got)
	}
}
