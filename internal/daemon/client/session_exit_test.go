package client

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

type SessionExitRPC struct {
	status  func(*daemon.SessionInfo) error
	pending func(*daemon.PendingRestartResp) error
}

func (s *SessionExitRPC) SessionStatus(_ *daemon.TaskIDReq, reply *daemon.SessionInfo) error {
	return s.status(reply)
}
func (s *SessionExitRPC) HasPendingRestart(_ *daemon.TaskIDReq, reply *daemon.PendingRestartResp) error {
	return s.pending(reply)
}

func sessionExitClient(t *testing.T, service *SessionExitRPC) *Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	testutil.NoError(t, server.RegisterName("Daemon", service))
	done := make(chan struct{})
	go func() { defer close(done); server.ServeCodec(jsonrpc.NewServerCodec(serverConn)) }()
	c := &Client{rpc: rpc.NewClientWithCodec(jsonrpc.NewClientCodec(clientConn))}
	t.Cleanup(func() { testutil.NoError(t, c.rpc.Close()); serverConn.Close(); <-done })
	return c
}

func TestSessionExit_Confirmation(t *testing.T) {
	for _, kind := range []string{"absent", "exits", "replaced", "restart", "status error", "restart error"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			service := &SessionExitRPC{status: func(info *daemon.SessionInfo) error {
				call := calls.Add(1)
				switch kind {
				case "status error":
					return errors.New("status unavailable")
				case "replaced":
					info.Alive = true
					info.PID = 2
				case "exits":
					info.Alive = call == 1
					info.PID = 1
				}
				return nil
			}, pending: func(reply *daemon.PendingRestartResp) error {
				if kind == "restart error" {
					return errors.New("restart unavailable")
				}
				reply.Pending = kind == "restart"
				return nil
			}}
			c := sessionExitClient(t, service)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := c.WaitForSessionExit(ctx, "task", 1)
			if kind == "absent" || kind == "exits" {
				testutil.NoError(t, err)
			} else {
				testutil.True(t, err != nil)
			}
			if kind == "exits" {
				testutil.Equal(t, calls.Load(), int32(2))
			}
		})
	}
}

func TestSessionExit_Cancellation(t *testing.T) {
	release := make(chan struct{})
	c := sessionExitClient(t, &SessionExitRPC{status: func(*daemon.SessionInfo) error { <-release; return nil }})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	testutil.ErrorIs(t, c.WaitForSessionExit(ctx, "task", 1), context.DeadlineExceeded)
	testutil.ErrorIs(t, (*Client)(nil).WaitForSessionExit(context.Background(), "task", 1), ErrClientClosed)
}

var _ agent.SessionExitWaiter = (*Client)(nil)

func TestSessionExit_Presence(t *testing.T) {
	for _, kind := range []string{"absent", "alive", "restart", "status error", "restart error"} {
		t.Run(kind, func(t *testing.T) {
			c := sessionExitClient(t, &SessionExitRPC{status: func(reply *daemon.SessionInfo) error {
				if kind == "status error" {
					return errors.New("status unavailable")
				}
				reply.Alive = kind == "alive"
				return nil
			}, pending: func(reply *daemon.PendingRestartResp) error {
				if kind == "restart error" {
					return errors.New("restart unavailable")
				}
				reply.Pending = kind == "restart"
				return nil
			}})
			present, err := c.SessionPresent(context.Background(), "task")
			if kind == "status error" || kind == "restart error" {
				testutil.True(t, err != nil)
			} else {
				testutil.NoError(t, err)
				testutil.Equal(t, present, kind == "alive" || kind == "restart")
			}
		})
	}
}

var _ agent.SessionPresenceChecker = (*Client)(nil)
