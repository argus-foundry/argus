package main

import (
	"encoding/json"
	"errors"
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
		{"stale launch", `{"hook_event_name":"PermissionRequest"}`, "stale", []string{"g"}, false},
		{"missing private file", `{"hook_event_name":"PermissionRequest"}`, "g", []string{"g"}, true},
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
