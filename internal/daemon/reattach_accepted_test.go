package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/mcp"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestReattach_AcceptedWorkerReplyStaysComplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	orch, err := d.db.CreateHeraOrchestrator("accepted", "")
	testutil.NoError(t, err)
	worker := &model.Task{Name: "worker", Worktree: t.TempDir(), Status: model.StatusInProgress}
	coord := &model.Task{Name: "coord", Worktree: t.TempDir(), Status: model.StatusInProgress}
	testutil.NoError(t, d.db.Add(worker))
	testutil.NoError(t, d.db.Add(coord))
	for _, entry := range []struct {
		task *model.Task
		kind db.HeraRoleKind
		name string
	}{{worker, db.HeraKindWorker, "worker"}, {coord, db.HeraKindCoordinator, "coord"}} {
		_, _, err = d.db.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: entry.name, Kind: entry.kind}, entry.task.ID, entry.task.Worktree)
		testutil.NoError(t, err)
	}
	s := mcp.New(d.db, 0, "")
	s.SetTaskManager(func(mcp.TaskCreateInput) (*model.Task, error) { return nil, fmt.Errorf("unused") }, d.db, agent.NewRunner(nil))
	s.SetHeraService(hera.New(d.db, nil), d.db, nil)
	call := func(name string, args map[string]string) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		testutil.NoError(t, err)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body)))
		var response struct {
			Error  any                `json:"error"`
			Result mcp.ToolCallResult `json:"result"`
		}
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		testutil.Nil(t, response.Error)
		testutil.Equal(t, response.Result.IsError, false)
	}
	call("hera_status", map[string]string{"cwd": worker.Worktree, "status": "done"})
	call("hera_accept", map[string]string{"cwd": coord.Worktree, "role_name": "worker"})
	call("hera_send", map[string]string{"cwd": worker.Worktree, "status": "working", "body": "More work remains", "tldr": "More work remains"})
	before, err := d.db.Get(worker.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, before.Status, model.StatusComplete)
	d.UseSupervisorRunner(&fakeSupClient{running: []string{worker.ID}})
	d.ReconcileOnStartup()
	after, err := d.db.Get(worker.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.Status, model.StatusComplete)
}
