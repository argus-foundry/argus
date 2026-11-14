package client

import (
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type lateStartService struct {
	release chan struct{}
	calls   atomic.Int32
}

func (s *lateStartService) StartSession(_ *daemon.StartReq, resp *daemon.StartResp) error {
	n := s.calls.Add(1)
	if n == 1 {
		<-s.release
	}
	resp.PID = 100 + int(n)
	return nil
}

func TestStartReceipt_LateResultAndReplacement(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "late result", true: "newer start wins"}[replace], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := net.Pipe()
				svc := &lateStartService{release: make(chan struct{})}
				server := rpc.NewServer()
				testutil.NoError(t, server.RegisterName("Daemon", svc))
				go server.ServeCodec(jsonrpc.NewServerCodec(b))
				c := &Client{rpc: jsonrpc.NewClient(a), sessions: map[string]*RemoteSession{}, closed: make(chan struct{}), peerProtocol: 14}
				t.Cleanup(func() { _ = c.Close() })
				task := &model.Task{ID: "t", SessionID: "saved", ResumeOnly: true}
				_, err := c.Start(task, config.DefaultConfig(), 24, 80, true)
				testutil.ErrorIs(t, err, agent.ErrStartAmbiguous)
				testutil.Equal(t, task.StartSequence, uint64(1))
				seq, pid, pending := c.StartReceipt("t")
				testutil.Equal(t, seq, uint64(1))
				testutil.Equal(t, pid, 0)
				testutil.True(t, pending)
				if replace {
					_, err = c.Start(task, config.DefaultConfig(), 24, 80, true)
					testutil.NoError(t, err)
				}
				close(svc.release)
				synctest.Wait()
				seq, pid, pending = c.StartReceipt("t")
				testutil.False(t, pending)
				if replace {
					testutil.Equal(t, seq, uint64(2))
					testutil.Equal(t, pid, 102)
				} else {
					testutil.Equal(t, seq, uint64(1))
					testutil.Equal(t, pid, 101)
				}
				_ = c.Close()
				synctest.Wait()
			})
		})
	}
}

func TestStartOnlyGuard(t *testing.T) {
	for _, peer := range []int{0, 13, 14} {
		t.Run(strconv.Itoa(peer), func(t *testing.T) {
			c, rec := newWireRecorderClient(t)
			t.Cleanup(func() { _ = c.Close() })
			c.peerProtocol = peer
			task := &model.Task{ID: "t", SessionID: "saved", ResumeOnly: true}
			_, err := c.Start(task, config.DefaultConfig(), 24, 80, true)
			if peer < 14 {
				testutil.NotNil(t, err)
				testutil.Contains(t, err.Error(), "requires restart")
			} else {
				testutil.NoError(t, err)
				rec.mu.Lock()
				testutil.True(t, rec.start.ResumeOnly)
				rec.mu.Unlock()
			}
		})
	}
}

func TestStartReceipt_FailedCallNotPending(t *testing.T) {
	c := &Client{}
	_, err := c.Start(&model.Task{ID: "t"}, config.DefaultConfig(), 24, 80, false)
	testutil.ErrorIs(t, err, ErrClientClosed)
	seq, pid, pending := c.StartReceipt("t")
	testutil.Equal(t, seq, uint64(1))
	testutil.Equal(t, pid, 0)
	testutil.False(t, pending)
}
