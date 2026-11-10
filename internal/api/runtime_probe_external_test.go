package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"net/rpc/jsonrpc"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/api"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/daemon/client"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type checkedRestartProbe interface {
	ProbeRestart(string) (bool, bool, error)
}

type runtimeRPCSession struct{ agent.SessionHandle }

func (*runtimeRPCSession) Alive() bool         { return true }
func (*runtimeRPCSession) IsIdle() bool        { return true }
func (*runtimeRPCSession) PTYSize() (int, int) { return 80, 24 }

type runtimeRPCProvider struct {
	agent.SessionRunner
	client *client.Client
	kicked bool
	starts int
}

func (r *runtimeRPCProvider) HasPendingRestart(id string) bool {
	if !r.kicked {
		return false
	}
	return r.client.HasPendingRestart(id)
}
func (r *runtimeRPCProvider) Get(id string) agent.SessionHandle {
	if !r.kicked {
		return &runtimeRPCSession{}
	}
	return r.client.Get(id)
}
func (r *runtimeRPCProvider) KickRerender(*model.Task, config.Config, uint16, uint16) error {
	r.kicked = true
	return nil
}
func (r *runtimeRPCProvider) StartOrReattach(*model.Task, config.Config, uint16, uint16, bool) (agent.SessionHandle, bool, error) {
	r.starts++
	return nil, false, errors.New("must not launch")
}
func (r *runtimeRPCProvider) ProbeRestart(id string) (bool, bool, error) {
	p, ok := any(r.client).(checkedRestartProbe)
	if !ok {
		return false, false, errors.New("checked client probe unavailable")
	}
	return p.ProbeRestart(id)
}

type RuntimeProbeRPC struct {
	mode         string
	pendingCalls atomic.Int32
	statusCalls  atomic.Int32
}

func (p *RuntimeProbeRPC) HasPendingRestart(*daemon.TaskIDReq, *daemon.PendingRestartResp) error {
	p.pendingCalls.Add(1)
	if p.mode == "timeout" {
		time.Sleep(2100 * time.Millisecond)
		return nil
	}
	if p.mode == "pending" {
		return errors.New("pending probe unavailable")
	}
	return nil
}
func (p *RuntimeProbeRPC) SessionStatus(_ *daemon.TaskIDReq, out *daemon.SessionInfo) error {
	p.statusCalls.Add(1)
	if p.mode == "late" {
		time.Sleep(2100 * time.Millisecond)
	}
	if p.mode == "status" || p.mode == "pending" || p.mode == "timeout" {
		return errors.New("session probe unavailable")
	}
	out.Alive = p.mode == "live"
	return nil
}

func TestRTProbe(t *testing.T) {
	for _, mode := range []string{"pending", "status", "timeout", "late", "absent", "live"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "p"))
			testutil.NoError(t, err)
			defer func() { _ = listener.Close() }()
			peer := &RuntimeProbeRPC{mode: mode}
			server := rpc.NewServer()
			testutil.NoError(t, server.RegisterName("Daemon", peer))
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = io.CopyN(io.Discard, conn, 1)
				server.ServeCodec(jsonrpc.NewServerCodec(conn))
			}()
			c, err := client.Connect(listener.Addr().String())
			testutil.NoError(t, err)
			defer func() { _ = c.Close(); <-done }()
			d, err := db.OpenInMemory()
			testutil.NoError(t, err)
			defer func() { _ = d.Close() }()
			testutil.NoError(t, d.SetBackend("plain", config.Backend{Command: "sleep 30"}))
			task := &model.Task{Name: "probe", Backend: "plain", Prompt: "original", Worktree: t.TempDir()}
			testutil.NoError(t, d.Add(task))
			runner := &runtimeRPCProvider{SessionRunner: agent.NewRunner(nil), client: c}
			srv := api.New(d, runner, "master", nil, nil)
			defer func() { _ = srv.Shutdown(context.Background()) }()
			mux := api.RuntimeRoutesForTest(srv)
			get := httptest.NewRecorder()
			mux.ServeHTTP(get, httptest.NewRequest("GET", "/api/tasks/"+task.ID+"/runtime", nil))
			var current map[string]any
			testutil.NoError(t, json.Unmarshal(get.Body.Bytes(), &current))
			req := httptest.NewRequest("PATCH", "/api/tasks/"+task.ID+"/runtime", strings.NewReader(`{"prompt":"replacement","apply":"restart"}`))
			req.Header.Set("If-Match", current["revision"].(string))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			var body map[string]any
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			t.Logf("probe %s: status=%d body=%s", mode, w.Code, w.Body.String())
			if mode == "live" {
				testutil.Equal(t, w.Code, http.StatusOK)
				testutil.Equal(t, body["started"], any(true))
			} else {
				testutil.Equal(t, w.Code, http.StatusInternalServerError)
				code := "restart_unknown"
				if mode == "absent" {
					code = "restart_failed"
				}
				testutil.Equal(t, body["code"], any(code))
				testutil.Equal(t, body["settings_applied"], any(true))
				testutil.Equal(t, body["rolled_back"], any(false))
				testutil.Equal(t, body["started"], any(false))
			}
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Prompt, "replacement")
			testutil.Equal(t, runner.starts, 0)
			_, checked := any(c).(checkedRestartProbe)
			testutil.True(t, checked)
			testutil.True(t, peer.pendingCalls.Load() > 0)
		})
	}
}
