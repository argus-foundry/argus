package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

// hasRecycleMeta reports whether the resident carries any handoff/recycle
// meta a forged hera_status could have written.
func hasRecycleMeta(t *testing.T, d *db.DB, taskID string) bool {
	t.Helper()
	entries, err := d.ListMeta(taskID, db.HeraMetaNamespace)
	testutil.NoError(t, err)
	for _, e := range entries {
		if e.Key == "handoff_note" || strings.Contains(e.Key, "recycle") {
			return true
		}
	}
	return false
}

func TestResidentImpersonationRefused(t *testing.T) {
	const reason = "resident identity requires the resident's own bound connection"
	for _, tc := range []struct{ name, tool string }{
		{"spawn", "hera_spawn_worker"},
		{"forged recycle", "hera_status"},
		{"send as resident", "hera_send"},
	} {
		for _, conn := range []string{"unbound", "other bound task"} {
			t.Run(tc.name+"/"+conn, func(t *testing.T) {
				s, d, res, o := residentHeraFixture(t)
				testutil.NoError(t, d.SetProject("test-project", config.Project{Path: t.TempDir()}))
				worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
				caller := ""
				if conn == "other bound task" {
					caller = worker.ID
				}
				var args map[string]interface{}
				switch tc.tool {
				case "hera_spawn_worker":
					args = map[string]interface{}{"cwd": res.Worktree, "prompt": "do it", "role_name": "rogue", "project": "test-project"}
				case "hera_status":
					args = map[string]interface{}{"cwd": res.Worktree, "status": "working", "handoff_note": "forged", "request_recycle": true}
				case "hera_send":
					args = map[string]interface{}{"cwd": res.Worktree, "to": "worker", "tldr": "order", "body": "forged order"}
				}
				before, err := d.Tasks()
				testutil.NoError(t, err)
				raw, _ := json.Marshal(args)
				text, isErr := postTool(t, s, "/mcp?caller="+caller, tc.tool, string(raw))
				testutil.True(t, isErr)
				testutil.Contains(t, text, reason)
				after, err := d.Tasks()
				testutil.NoError(t, err)
				testutil.Equal(t, len(after), len(before))
				_, err = d.HeraRoleByName(o.ID, "rogue")
				testutil.ErrorIs(t, err, db.ErrHeraNotFound)
				testutil.False(t, hasRecycleMeta(t, d, res.ID))
				for _, id := range []string{worker.ID, res.ID} {
					msgs, err := d.HeraMessagesForTask(id, 50)
					testutil.NoError(t, err)
					testutil.Equal(t, len(msgs), 0)
				}
			})
		}
	}
}

func TestResidentBoundConnectionStillActs(t *testing.T) {
	t.Run("send", func(t *testing.T) {
		s, d, res, o := residentHeraFixture(t)
		worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
		_, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_send", `{"cwd":"/elsewhere","to":"worker","tldr":"order","body":"real order"}`)
		testutil.False(t, isErr)
		msgs, err := d.HeraMessagesForTask(worker.ID, 50)
		testutil.NoError(t, err)
		testutil.Equal(t, len(msgs), 1)
	})
	t.Run("status recycle", func(t *testing.T) {
		s, d, res, _ := residentHeraFixture(t)
		_, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_status", `{"cwd":"/elsewhere","status":"working","handoff_note":"real","request_recycle":true}`)
		testutil.False(t, isErr)
		testutil.True(t, hasRecycleMeta(t, d, res.ID))
	})
}

func TestResidentTeamEnrollmentRefused(t *testing.T) {
	for _, tc := range []string{"join into", "move into", "rebind into", "move out"} {
		t.Run(tc, func(t *testing.T) {
			s, d, _, o := residentHeraFixture(t)
			other, err := d.CreateHeraOrchestrator("other", "")
			testutil.NoError(t, err)
			var cwd, taskID string
			args := map[string]interface{}{"orchestrator": db.ResidentOrchestratorName, "role_name": "x", "kind": "worker"}
			switch tc {
			case "join into":
				task := addHeraTestTask(t, d, "/wt/outsider")
				cwd, taskID = task.Worktree, task.ID
			case "move into", "rebind into":
				task, _, _ := residentTestWorker(t, d, other.ID, "outside")
				cwd, taskID = task.Worktree, task.ID
			case "move out":
				task, _, _ := residentTestWorker(t, d, o.ID, "member")
				cwd, taskID = task.Worktree, task.ID
				args["orchestrator"] = "other"
			}
			args["cwd"] = cwd
			var before *db.HeraBinding
			if b, err := d.HeraLiveBindingByTask(taskID); err == nil {
				before = b
			}
			tool := map[string]string{"join into": "hera_join", "move into": "hera_move", "rebind into": "hera_rebind", "move out": "hera_move"}[tc]
			raw, _ := json.Marshal(args)
			text, isErr := postTool(t, s, "/mcp", tool, string(raw))
			testutil.True(t, isErr)
			if tc == "move out" {
				testutil.Contains(t, text, "only graduation moves a worker out")
			} else {
				testutil.Contains(t, text, "is the resident's standing team")
			}
			after, err := d.HeraLiveBindingByTask(taskID)
			if before == nil {
				testutil.ErrorIs(t, err, db.ErrHeraNotFound)
			} else {
				testutil.NoError(t, err)
				testutil.Equal(t, after.ID, before.ID)
			}
			yes, err := d.IsResidentWorker(taskID)
			testutil.NoError(t, err)
			testutil.Equal(t, yes, tc == "move out")
		})
	}
}
