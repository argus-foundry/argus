package mcp

import (
	"encoding/json"
	"testing"

	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

// residentTaskDB layers a ResidentTask lookup over the shared mock, like
// *db.DB does, while its Tasks() deliberately still returns the resident row
// so the MCP layer's own exclusion is what the tests exercise.
type residentTaskDB struct {
	*mockTaskDB
	resident *model.Task
}

func (r *residentTaskDB) ResidentTask() (*model.Task, error) { return r.resident, nil }

func residentServer(t *testing.T) (*Server, *residentTaskDB) {
	t.Helper()
	s, base, stopper := testServerWithTasks()
	res := &model.Task{ID: "res-1", Name: "resident", Kind: model.KindResident,
		Status: model.StatusInProgress, Worktree: "/home/x/.argus/resident"}
	base.tasks = append(base.tasks, res)
	rdb := &residentTaskDB{mockTaskDB: base, resident: res}
	s.SetTaskManager(func(TaskCreateInput) (*model.Task, error) { return nil, nil }, rdb, stopper)
	return s, rdb
}

func callTool(t *testing.T, s *Server, name, args string) (string, bool) {
	t.Helper()
	resp := doRequest(t, s, "tools/call", ToolCallParams{Name: name, Arguments: json.RawMessage(args)})
	testutil.NoError(t, respErr(resp))
	cr := callResult(t, resp)
	return cr.Content[0].Text, cr.IsError
}

func TestResident_TaskListOmitsIt(t *testing.T) {
	s, _ := residentServer(t)
	text, isErr := callTool(t, s, "task_list", `{}`)
	testutil.Equal(t, isErr, false)
	if contains(text, "res-1") || contains(text, "resident") {
		t.Fatalf("task_list leaked the resident: %s", text)
	}
}

func TestResident_TaskGetIsNotFound(t *testing.T) {
	s, _ := residentServer(t)
	text, isErr := callTool(t, s, "task_get", `{"id":"res-1"}`)
	testutil.Equal(t, isErr, true)
	testutil.Contains(t, text, "task not found")
}

func TestResident_LifecycleToolsReject(t *testing.T) {
	for _, tool := range []string{"task_complete", "task_archive", "task_rename"} {
		t.Run(tool, func(t *testing.T) {
			s, rdb := residentServer(t)
			args := `{"id":"res-1","name":"new"}`
			text, isErr := callTool(t, s, tool, args)
			testutil.Equal(t, isErr, true)
			testutil.Contains(t, text, "resident")
			testutil.Equal(t, rdb.resident.Status, model.StatusInProgress)
			testutil.Equal(t, rdb.resident.Archived, false)
			testutil.Equal(t, rdb.resident.Name, "resident")
		})
	}
}

func TestResident_ResolveTaskByCwdFindsResidentForSelfTools(t *testing.T) {
	s, rdb := residentServer(t)
	got, err := s.resolveTask("", "/home/x/.argus/resident")
	testutil.NoError(t, err)
	testutil.Equal(t, got.ID, rdb.resident.ID)

	// A sub-directory of the resident dir resolves too.
	got, err = s.resolveTask("", "/home/x/.argus/resident/sub")
	testutil.NoError(t, err)
	testutil.Equal(t, got.ID, rdb.resident.ID)

	// An unrelated cwd still fails.
	if _, err := s.resolveTask("", "/nowhere"); err == nil {
		t.Fatal("expected no match")
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
