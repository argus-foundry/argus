package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentPermissionHookDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, input, state string
		allow, fail        bool
		want               string
	}{
		{"queued", `{"command":"echo probe"}`, "pending", false, false, "Queued for Aaron"},
		{"allow", `{"body":"exact connector write","id":9007199254740993}`, "consumed", true, false, "allow"},
		{"denied", `{}`, "denied", false, false, "Aaron denied"},
		{"rpc failure", `{}`, "", false, true, "could not defer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := `{"hook_event_name":"PermissionRequest","tool_name":"mcp__test__write","session_id":"s","cwd":"/resident","tool_input":` + tc.input + `}`
			out := residentPermissionHook(strings.NewReader(event), []string{"generation"}, func() (agent.ResidentApprovalLaunch, error) {
				return agent.ResidentApprovalLaunch{Generation: "generation", Token: "private", TaskID: "resident"}, nil
			}, func(req *daemon.ResidentPermissionReq) (daemon.ResidentPermissionResp, error) {
				testutil.Equal(t, req.Call.Tool, "mcp__test__write")
				testutil.Equal(t, string(req.Call.Input), tc.input)
				testutil.Equal(t, req.Token, "private")
				if tc.fail {
					return daemon.ResidentPermissionResp{}, errors.New("unavailable")
				}
				return daemon.ResidentPermissionResp{Allow: tc.allow, State: tc.state, BoardID: 12}, nil
			})
			raw, err := json.Marshal(out)
			testutil.NoError(t, err)
			testutil.Contains(t, string(raw), tc.want)
			testutil.False(t, strings.Contains(string(raw), "private"))
			testutil.False(t, strings.Contains(string(raw), "updatedPermissions"))
			if !tc.allow {
				testutil.Contains(t, string(raw), `"interrupt":false`)
			}
		})
	}
}

type PermissionHookFake struct{ Reject bool }

func TestResidentPermissionHookLeavesOtherDialogsNative(t *testing.T) {
	for _, tool := range []string{"AskUserQuestion", "ExitPlanMode", "Read", "Agent"} {
		t.Run(tool, func(t *testing.T) {
			event := `{"hook_event_name":"PermissionRequest","tool_name":"` + tool + `","tool_input":{}}`
			out := residentPermissionHook(strings.NewReader(event), []string{"generation"}, func() (agent.ResidentApprovalLaunch, error) {
				t.Fatal("unrelated tool must not read credential")
				return agent.ResidentApprovalLaunch{}, nil
			}, func(*daemon.ResidentPermissionReq) (daemon.ResidentPermissionResp, error) {
				t.Fatal("unrelated tool must not queue")
				return daemon.ResidentPermissionResp{}, nil
			})
			testutil.Equal(t, len(out), 0)
		})
	}
}

func (f *PermissionHookFake) ResidentPermission(_ *daemon.ResidentPermissionReq, out *daemon.ResidentPermissionResp) error {
	if f.Reject {
		out.Error = "denied by fake"
	} else {
		out.Allow = true
		out.BoardID = 12
	}
	return nil
}

func TestPermRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(filepath.Dir(daemon.DefaultSocketPath()), 0o700))
	listener, err := net.Listen("unix", daemon.DefaultSocketPath())
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{true: "error", false: "allow"}[reject], func(t *testing.T) {
			server := rpc.NewServer()
			testutil.NoError(t, server.RegisterName("Daemon", &PermissionHookFake{Reject: reject}))
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close() //nolint:errcheck
				var prefix [1]byte
				_, err = io.ReadFull(conn, prefix[:])
				if err != nil {
					t.Error(err)
					return
				}
				testutil.Equal(t, prefix[0], byte('R'))
				server.ServeCodec(jsonrpc.NewServerCodec(conn))
			}()
			out, err := callResidentPermission(&daemon.ResidentPermissionReq{})
			if reject {
				testutil.Error(t, err)
			} else {
				testutil.NoError(t, err)
				testutil.True(t, out.Allow)
				testutil.Equal(t, out.BoardID, int64(12))
			}
			<-done
		})
	}
	testutil.NoError(t, listener.Close())
	_, err = callResidentPermission(&daemon.ResidentPermissionReq{})
	testutil.Error(t, err)
}

func TestResidentPermissionHookFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, event, generation string
		args                    []string
		readErr                 bool
	}{
		{"bad args", `{}`, "", nil, false},
		{"malformed", `x`, "", []string{"g"}, false},
		{"oversized", strings.Repeat(" ", 65537), "", []string{"g"}, false},
		{"wrong event", `{"hook_event_name":"PreToolUse"}`, "g", []string{"g"}, false},
		{"stale launch", `{"hook_event_name":"PermissionRequest","tool_name":"Bash"}`, "stale", []string{"g"}, false},
		{"missing private file", `{"hook_event_name":"PermissionRequest","tool_name":"Bash"}`, "g", []string{"g"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := residentPermissionHook(strings.NewReader(tc.event), tc.args, func() (agent.ResidentApprovalLaunch, error) {
				if tc.readErr {
					return agent.ResidentApprovalLaunch{}, errors.New("missing")
				}
				return agent.ResidentApprovalLaunch{Generation: tc.generation}, nil
			}, func(*daemon.ResidentPermissionReq) (daemon.ResidentPermissionResp, error) {
				t.Fatal("unexpected daemon call")
				return daemon.ResidentPermissionResp{}, nil
			})
			raw, _ := json.Marshal(out)
			testutil.Contains(t, string(raw), `"behavior":"deny"`)
		})
	}
}

func TestQueuedApprovalMessage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resp    daemon.ResidentPermissionResp
		want    []string
		notWant []string
	}{
		{"new", daemon.ResidentPermissionResp{BoardID: 574}, []string{"Queued for Aaron's approval as #574; nothing ran.", "hera_inbox", "timeout_seconds 120", "60 minutes", "hera_board_resolve on #574"}, []string{"Already queued", "replaces"}},
		{"reused", daemon.ResidentPermissionResp{BoardID: 574, Reused: true}, []string{"Already queued for Aaron's approval as #574 and still waiting"}, []string{"replaces"}},
		{"supersedes", daemon.ResidentPermissionResp{BoardID: 574, Superseded: []int64{561, 562}}, []string{"It replaces #561, #562, now closed."}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := queuedApprovalMessage(tc.resp)
			for _, w := range tc.want {
				testutil.Contains(t, msg, w)
			}
			for _, w := range tc.notWant {
				testutil.False(t, strings.Contains(msg, w))
			}
		})
	}
}
