package client

import (
	"errors"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"sync/atomic"
	"testing"

	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

type RestartProbeRPC struct {
	pending, live         bool
	pendingErr, statusErr bool
	statusCalls           atomic.Int32
}

func (p *RestartProbeRPC) HasPendingRestart(_ *daemon.TaskIDReq, out *daemon.PendingRestartResp) error {
	if p.pendingErr {
		return errors.New("pending unavailable")
	}
	out.Pending = p.pending
	return nil
}
func (p *RestartProbeRPC) SessionStatus(_ *daemon.TaskIDReq, out *daemon.SessionInfo) error {
	p.statusCalls.Add(1)
	if p.statusErr {
		return errors.New("session unavailable")
	}
	out.Alive = p.live
	return nil
}

func TestProbeRestart(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		pending, live, pendingErr, statusErr bool
	}{
		{name: "absent"}, {name: "live", live: true}, {name: "pending", pending: true},
		{name: "pending error", pendingErr: true}, {name: "session error", statusErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := &RestartProbeRPC{pending: tc.pending, live: tc.live, pendingErr: tc.pendingErr, statusErr: tc.statusErr}
			server := rpc.NewServer()
			testutil.NoError(t, server.RegisterName("Daemon", peer))
			local, remote := net.Pipe()
			done := make(chan struct{})
			go func() { defer close(done); server.ServeCodec(jsonrpc.NewServerCodec(remote)) }()
			transport := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(local))
			defer func() { _ = transport.Close(); <-done }()
			c := &Client{rpc: transport}
			live, pending, err := c.ProbeRestart("task")
			testutil.Equal(t, err != nil, tc.pendingErr || tc.statusErr)
			testutil.Equal(t, live, tc.live)
			testutil.Equal(t, pending, tc.pending)
			calls := int32(1)
			if tc.pending || tc.pendingErr {
				calls = 0
			}
			testutil.Equal(t, peer.statusCalls.Load(), calls)
		})
	}
	t.Run("disconnected", func(t *testing.T) {
		_, _, err := new(Client).ProbeRestart("task")
		testutil.ErrorIs(t, err, ErrClientClosed)
	})
}
