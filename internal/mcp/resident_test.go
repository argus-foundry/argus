package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
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

// postTool sends a tools/call over HTTP to the given path (query included) so
// the connection-bound caller identity is exercised end to end.
func postTool(t *testing.T, s *Server, path, tool, args string) (string, bool) {
	t.Helper()
	body, err := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"` + tool + `","arguments":` + args + `}`)})
	testutil.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var resp Response
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	cr := callResult(t, &resp)
	return cr.Content[0].Text, cr.IsError
}

func TestResident_TaskRecycleBoundToCaller(t *testing.T) {
	newSrv := func() (*Server, *[]TaskRecycleInput) {
		s, _ := residentServer(t)
		var got []TaskRecycleInput
		s.SetTaskRecycler(func(in TaskRecycleInput) error { got = append(got, in); return nil })
		return s, &got
	}
	const note = `"handoff_note":"x"`

	t.Run("resident caller may recycle itself", func(t *testing.T) {
		s, got := newSrv()
		text, isErr := postTool(t, s, "/mcp?caller=res-1", "task_recycle", `{"cwd":"/home/x/.argus/resident",`+note+`}`)
		testutil.Equal(t, isErr, false)
		testutil.Equal(t, len(*got), 1)
		testutil.Equal(t, (*got)[0].TaskID, "res-1")
		_ = text
	})
	t.Run("resident caller cannot recycle another task by id", func(t *testing.T) {
		s, got := newSrv()
		text, isErr := postTool(t, s, "/mcp?caller=res-1", "task_recycle", `{"id":"abc123",`+note+`}`)
		testutil.Equal(t, isErr, true)
		testutil.Contains(t, text, "resident")
		testutil.Equal(t, len(*got), 0)
	})
	t.Run("resident caller cannot recycle another task by cwd", func(t *testing.T) {
		s, got := newSrv()
		_, isErr := postTool(t, s, "/mcp?caller=res-1", "task_recycle", `{"cwd":"/tmp/worktrees/myapp/fix-login",`+note+`}`)
		testutil.Equal(t, isErr, true)
		testutil.Equal(t, len(*got), 0)
	})
	t.Run("unbound caller cannot recycle the resident", func(t *testing.T) {
		s, got := newSrv()
		text, isErr := postTool(t, s, "/mcp", "task_recycle", `{"cwd":"/home/x/.argus/resident",`+note+`}`)
		testutil.Equal(t, isErr, true)
		testutil.Contains(t, text, "only the resident agent may recycle itself")
		testutil.Equal(t, len(*got), 0)
		_, isErr = postTool(t, s, "/mcp", "task_recycle", `{"id":"res-1",`+note+`}`)
		testutil.Equal(t, isErr, true)
		testutil.Equal(t, len(*got), 0)
	})
	t.Run("another bound task cannot recycle the resident", func(t *testing.T) {
		s, got := newSrv()
		_, isErr := postTool(t, s, "/mcp?caller=abc123", "task_recycle", `{"id":"res-1",`+note+`}`)
		testutil.Equal(t, isErr, true)
		testutil.Equal(t, len(*got), 0)
	})
	t.Run("no caller identity leaves other tasks unrestricted", func(t *testing.T) {
		s, got := newSrv()
		_, isErr := postTool(t, s, "/mcp", "task_recycle", `{"id":"abc123",`+note+`}`)
		testutil.Equal(t, isErr, false)
		testutil.Equal(t, len(*got), 1)
	})
	t.Run("any bound caller is limited to itself, with no resident lookup needed", func(t *testing.T) {
		s, got := newSrv()
		s.taskDB = &mockTaskDB{tasks: []*model.Task{{ID: "abc123", Worktree: "/w/a"}, {ID: "def456", Worktree: "/w/d"}}} // no ResidentTask lookup
		_, isErr := postTool(t, s, "/mcp?caller=abc123", "task_recycle", `{"id":"def456",`+note+`}`)
		testutil.Equal(t, isErr, true)
		testutil.Equal(t, len(*got), 0)
		_, isErr = postTool(t, s, "/mcp?caller=abc123", "task_recycle", `{"id":"abc123",`+note+`}`)
		testutil.Equal(t, isErr, false)
	})
}
