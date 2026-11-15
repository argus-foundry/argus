package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc/jsonrpc"
	"os"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/db"
)

type permissionHookEvent struct {
	Event   string          `json:"hook_event_name"`
	Session string          `json:"session_id"`
	Cwd     string          `json:"cwd"`
	Tool    string          `json:"tool_name"`
	Input   json.RawMessage `json:"tool_input"`
	Server  json.RawMessage `json:"mcp_server"`
}

func runResidentPermissionHook(args []string) {
	out := residentPermissionHook(os.Stdin, args, agent.ReadResidentApprovalLaunch, callResidentPermission)
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func residentPermissionHook(input io.Reader, args []string, read func() (agent.ResidentApprovalLaunch, error), call func(*daemon.ResidentPermissionReq) (daemon.ResidentPermissionResp, error)) map[string]any {
	deny := func(message string) map[string]any {
		return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": map[string]any{"behavior": "deny", "message": message, "interrupt": false}}}
	}
	failed := "Argus could not defer this permission request. The call was denied; continue other work and report the hook failure."
	if len(args) != 1 {
		return deny(failed)
	}
	raw, err := io.ReadAll(io.LimitReader(input, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return deny(failed)
	}
	var event permissionHookEvent
	if json.Unmarshal(raw, &event) != nil || event.Event != "PermissionRequest" {
		return deny(failed)
	}
	launch, err := read()
	if err != nil || launch.Generation != args[0] {
		return deny(failed)
	}
	resp, err := call(&daemon.ResidentPermissionReq{Token: launch.Token, Call: db.ApprovalCall{TaskID: launch.TaskID, Launch: launch.Generation, Session: event.Session, Cwd: event.Cwd, Tool: event.Tool, Input: event.Input, Server: event.Server}})
	if err != nil || resp.Error != "" {
		return deny(failed)
	}
	if resp.Allow {
		return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": map[string]any{"behavior": "allow"}}}
	}
	if resp.BoardID <= 0 {
		return deny(failed)
	}
	if resp.State == "denied" {
		return deny(fmt.Sprintf("Aaron denied deferred call #%d. Do not retry it. Continue other work.", resp.BoardID))
	}
	return deny(fmt.Sprintf("Queued for Aaron's approval as #%d. Carry on with other work; do not retry or find another route around this approval. You will receive an [argus] message when he answers.", resp.BoardID))
}

func callResidentPermission(req *daemon.ResidentPermissionReq) (daemon.ResidentPermissionResp, error) {
	var resp daemon.ResidentPermissionResp
	conn, err := net.DialTimeout("unix", daemon.DefaultSocketPath(), 2*time.Second)
	if err != nil {
		return resp, err
	}
	if _, err := conn.Write([]byte{'R'}); err != nil {
		_ = conn.Close()
		return resp, err
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = conn.Close()
		return resp, err
	}
	client := jsonrpc.NewClient(conn)
	defer client.Close() //nolint:errcheck
	err = client.Call("Daemon.ResidentPermission", req, &resp)
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, err
}
