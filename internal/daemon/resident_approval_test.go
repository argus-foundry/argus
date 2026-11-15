package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentPermissionAuthenticatedQueueAndRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	r, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = d.EnsureResidentOrchestrator(r.ID)
	testutil.NoError(t, err)
	launch := agent.ResidentApprovalLaunch{Token: "test-private", Generation: agent.HashResidentMCPToken("test-private"), TaskID: r.ID, Session: "s", Cwd: r.Worktree}
	raw, err := json.Marshal(launch)
	testutil.NoError(t, err)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(agent.ResidentApprovalLaunchPath()), 0o700))
	testutil.NoError(t, os.WriteFile(agent.ResidentApprovalLaunchPath(), raw, 0o600))
	da := New(d)
	svc := &RPCService{daemon: da}
	req := ResidentPermissionReq{Token: launch.Token, Call: db.ApprovalCall{TaskID: r.ID, Launch: launch.Generation, Session: "s", Cwd: r.Worktree, Tool: "mcp__test__write", Input: json.RawMessage(`{"body":"exact"}`)}}
	var resp ResidentPermissionResp
	testutil.NoError(t, svc.ResidentPermission(&req, &resp))
	testutil.Equal(t, resp.Error, "")
	testutil.False(t, resp.Allow)
	a, _, err := d.RequestResidentApproval(req.Call, time.Now())
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, launch.Generation, true, time.Now()))
	testutil.NoError(t, svc.ResidentPermission(&req, &resp))
	testutil.True(t, resp.Allow)
	testutil.NoError(t, svc.ResidentPermission(&req, &resp))
	testutil.False(t, resp.Allow)
	// The hook credential and request session still agree; only the task's
	// current session differs, so the database identity check must reject it.
	r.SessionID = "new-current-session"
	testutil.NoError(t, d.Update(r))
	testutil.Error(t, validateResidentPermission(d, launch, &req))
	r.SessionID = "s"
	testutil.NoError(t, d.Update(r))
	for _, name := range []string{"token", "generation", "session", "cwd", "task", "account", "malformed input"} {
		t.Run(name, func(t *testing.T) {
			bad := req
			switch name {
			case "token":
				bad.Token = "forged"
			case "generation":
				bad.Call.Launch = "stale"
			case "session":
				bad.Call.Session = "other"
			case "cwd":
				bad.Call.Cwd += "/other"
			case "task":
				bad.Call.TaskID = "foreign"
			case "account":
				launch.Account = "other"
				testutil.Error(t, validateResidentPermission(d, launch, &bad))
				return
			case "malformed input":
				bad.Call.Input = json.RawMessage(`{"x":1,"x":2}`)
			}
			var out ResidentPermissionResp
			testutil.NoError(t, svc.ResidentPermission(&bad, &out))
			testutil.False(t, out.Allow)
			testutil.True(t, out.Error != "")
		})
	}
}
