package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// armedResidentTokens maps a test *Server to the resident token its fixture
// armed; postTool presents it the way the resident's private MCP config does.
var armedResidentTokens sync.Map

// armResidentToken points s at a temp hash file holding a fresh token's hash
// (never the real ~/.argus) and records the token for postTool. Calling it
// again rotates: the previous token stops verifying.
func armResidentToken(t *testing.T, s *Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp-token.sha256")
	token := fmt.Sprintf("tok-%s-%d", t.Name(), len(path))
	if prev, ok := armedResidentTokens.Load(s); ok {
		token = prev.(string) + "-next"
		if p, err := s.residentTokenHashPath(); err == nil {
			path = p
		}
	}
	testutil.NoError(t, os.WriteFile(path, []byte(agent.HashResidentMCPToken(token)+"\n"), 0o600))
	s.residentTokenHashPath = func() (string, error) { return path, nil }
	armedResidentTokens.Store(s, token)
	t.Cleanup(func() { armedResidentTokens.Delete(s) })
	return token
}

// postRaw sends a JSON-RPC request with an explicit token header ("" sends
// none), bypassing the fixture's armed token.
func postRaw(t *testing.T, s *Server, path, token, method string, params any) *Response {
	t.Helper()
	p, err := json.Marshal(params)
	testutil.NoError(t, err)
	body, err := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: method, Params: p})
	testutil.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	if token != "" {
		req.Header.Set(agent.ResidentMCPTokenHeader, token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var resp Response
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

func listedTools(t *testing.T, resp *Response) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(resp.Result)
	testutil.NoError(t, err)
	var result ToolsListResult
	testutil.NoError(t, json.Unmarshal(raw, &result))
	out := map[string]bool{}
	for _, tool := range result.Tools {
		out[tool.Name] = true
	}
	return out
}

func TestResidentToken_ForgedCallerIsUnbound(t *testing.T) {
	for _, tc := range []string{"no token", "wrong token", "previous launch token", "current token"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			_, r, _ := residentTestWorker(t, d, o.ID, "worker")
			token := armedToken(t, s)
			switch tc {
			case "no token":
				token = ""
			case "wrong token":
				token = "not-the-token"
			case "previous launch token":
				armResidentToken(t, s) // rotation: a new launch rewrote the hash
			}
			path := "/mcp?caller=" + res.ID
			tools := listedTools(t, postRaw(t, s, path, token, "tools/list", map[string]any{}))
			verified := tc == "current token"
			testutil.Equal(t, tools["hera_delete_worker"], verified)
			testutil.Equal(t, tools["hera_graduate_worker"], verified)

			grad := callResult(t, postRaw(t, s, path, token, "tools/call", ToolCallParams{Name: "hera_graduate_worker", Arguments: json.RawMessage(`{"role":"worker"}`)}))
			if !verified {
				testutil.True(t, grad.IsError)
				testutil.Contains(t, grad.Content[0].Text, "hera_graduate_worker is resident only")
			}
			del := callResult(t, postRaw(t, s, path, token, "tools/call", ToolCallParams{Name: "hera_delete_worker", Arguments: json.RawMessage(`{"role":"worker","force":true}`)}))
			got, err := d.HeraRole(r.ID)
			testutil.NoError(t, err)
			if verified {
				testutil.False(t, del.IsError)
				testutil.Contains(t, del.Content[0].Text, "Deleted worker worker")
				testutil.True(t, got.NukedAt != nil)
				return
			}
			testutil.True(t, del.IsError)
			testutil.Contains(t, del.Content[0].Text, "hera_delete_worker is resident only")
			testutil.True(t, got.NukedAt == nil)
		})
	}
}

func TestResidentToken_RotationAcceptsOnlyNewest(t *testing.T) {
	s, _, res, _ := residentHeraFixture(t)
	first := armedToken(t, s)
	path := "/mcp?caller=" + res.ID
	testutil.True(t, listedTools(t, postRaw(t, s, path, first, "tools/list", map[string]any{}))["hera_delete_worker"])
	second := armResidentToken(t, s)
	testutil.False(t, listedTools(t, postRaw(t, s, path, first, "tools/list", map[string]any{}))["hera_delete_worker"])
	testutil.True(t, listedTools(t, postRaw(t, s, path, second, "tools/list", map[string]any{}))["hera_delete_worker"])
}

func TestResidentToken_TokenOnNonResidentCallerIsUnbound(t *testing.T) {
	s, d, _, o := residentHeraFixture(t)
	worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
	token := armedToken(t, s)
	tools := listedTools(t, postRaw(t, s, "/mcp?caller="+worker.ID, token, "tools/list", map[string]any{}))
	testutil.False(t, tools["hera_delete_worker"])
}

// armedToken returns the token a fixture armed for s.
func armedToken(t *testing.T, s *Server) string {
	t.Helper()
	v, ok := armedResidentTokens.Load(s)
	if !ok {
		t.Fatal("fixture armed no resident token")
	}
	return v.(string)
}

func TestResidentDelete_DirtyTreeRefusedWithoutForce(t *testing.T) {
	for _, tc := range []string{"dirty safe", "dirty safe force", "clean merged", "unknown tree"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			repo := t.TempDir()
			run := func(dir string, args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			run(repo, "init", "-b", "master")
			run(repo, "config", "user.name", "Test")
			run(repo, "config", "user.email", "test@example.com")
			run(repo, "config", "commit.gpgsign", "false")
			run(repo, "commit", "--allow-empty", "-m", "initial")
			branch := "argus/resident/work"
			wt := filepath.Join(db.DataDir(), "worktrees", "p", "work")
			run(repo, "worktree", "add", "-b", branch, wt)
			testutil.NoError(t, d.SetProject("p", config.Project{Path: repo, Branch: "master"}))
			task := &model.Task{ID: "work", Name: "work", Project: "p", Branch: branch, BaseBranch: "master", Worktree: wt, Status: model.StatusInProgress}
			testutil.NoError(t, d.Add(task))
			_, _, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker}, task.ID, wt)
			testutil.NoError(t, err)
			if tc != "clean merged" {
				// Zero commits ahead, plus an untracked file.
				testutil.NoError(t, os.WriteFile(filepath.Join(wt, "scratch"), []byte("work"), 0o600))
			}
			if tc == "unknown tree" {
				// git status fails: the dirty count is unknown, which fails closed.
				testutil.NoError(t, os.Remove(filepath.Join(wt, ".git")))
			}
			// The classifier calls the branch safe (merged / no commits ahead).
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.Verdict{Safe: true, Reason: "merged"}, nil
			}
			force := tc == "dirty safe force"
			text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_delete_worker", fmt.Sprintf(`{"role":"work","force":%t}`, force))
			refused := tc == "dirty safe" || tc == "unknown tree"
			testutil.Equal(t, isErr, refused)
			_, statErr := os.Stat(wt)
			if refused {
				testutil.Contains(t, text, "uncommitted or untracked")
				if tc == "dirty safe" {
					testutil.Contains(t, text, "dirty files: 1")
					_, statErr = os.Stat(filepath.Join(wt, "scratch"))
				} else {
					testutil.Contains(t, text, "dirty files: unknown (")
				}
				testutil.NoError(t, statErr)
				_, err = d.Get(task.ID)
				testutil.NoError(t, err)
				return
			}
			testutil.True(t, os.IsNotExist(statErr))
			_, err = d.Get(task.ID)
			testutil.ErrorIs(t, err, db.ErrTaskNotFound)
			if force {
				testutil.Contains(t, text, `"dirty_count": "1"`)
				testutil.Contains(t, text, branch)
			}
		})
	}
}

// countingStore records Inbox reads so a refused call proves it read nothing.
type countingStore struct {
	*mockMessageStore
	inboxReads int
}

func (c *countingStore) Inbox(toID string, f db.InboxFilter) ([]*model.TaskMessage, error) {
	c.inboxReads++
	return c.mockMessageStore.Inbox(toID, f)
}

func TestResidentIdentityGuard_EveryTool(t *testing.T) {
	type call struct{ tool, args string }
	for _, tc := range []struct {
		name string
		call call
	}{
		{"inbox by cwd", call{"task_inbox", `{"cwd":"/home/x/.argus/resident"}`}},
		{"inbox by cwd subdir", call{"task_inbox", `{"cwd":"/home/x/.argus/resident/memory"}`}},
		{"inbox by id", call{"task_inbox", `{"id":"res-1"}`}},
		{"send by cwd", call{"task_message_send", `{"cwd":"/home/x/.argus/resident","to":"abc123","body":"forged"}`}},
		{"send by id", call{"task_message_send", `{"id":"res-1","to":"abc123","body":"forged"}`}},
		{"ack by cwd", call{"task_message_ack", `{"cwd":"/home/x/.argus/resident","message_ids":["m1"]}`}},
		{"set result by cwd", call{"task_set_result", `{"cwd":"/home/x/.argus/resident","result":{"ok":true}}`}},
		{"set result by id", call{"task_set_result", `{"id":"res-1","result":{"ok":true}}`}},
		{"create as resident", call{"task_create", `{"caller_id":"res-1","prompt":"x","project":"myapp"}`}},
		{"create by resident cwd", call{"task_create", `{"cwd":"/home/x/.argus/resident","prompt":"x","project":"myapp"}`}},
	} {
		for _, conn := range []string{"unbound", "forged caller"} {
			t.Run(tc.name+"/"+conn, func(t *testing.T) {
				s, rdb := residentServer(t)
				store := &countingStore{mockMessageStore: &mockMessageStore{}}
				s.SetMessageManager(store, &mockNudger{})
				created := 0
				s.createTask = func(TaskCreateInput) (*model.Task, error) { created++; return &model.Task{ID: "new"}, nil }
				path := "/mcp"
				if conn == "forged caller" {
					path = "/mcp?caller=res-1" // resident id, no token
				}
				resp := postRaw(t, s, path, "", "tools/call", ToolCallParams{Name: tc.call.tool, Arguments: json.RawMessage(tc.call.args)})
				cr := callResult(t, resp)
				testutil.True(t, cr.IsError)
				testutil.Contains(t, cr.Content[0].Text, "resident identity requires the resident's own bound connection")
				testutil.Equal(t, store.inboxReads, 0)
				testutil.Equal(t, len(store.messages), 0)
				testutil.Equal(t, rdb.resident.Result, "")
				testutil.Equal(t, created, 0)
			})
		}
	}
}

func TestResidentIdentityGuard_VerifiedResidentStillActs(t *testing.T) {
	s, rdb := residentServer(t)
	store := &countingStore{mockMessageStore: &mockMessageStore{}}
	s.SetMessageManager(store, &mockNudger{})
	const cwd = `"cwd":"/home/x/.argus/resident"`

	_, isErr := postTool(t, s, "/mcp?caller=res-1", "task_message_send", `{`+cwd+`,"to":"abc123","body":"real"}`)
	testutil.False(t, isErr)
	testutil.Equal(t, len(store.messages), 1)
	testutil.Equal(t, store.messages[0].From, "res-1")

	_, isErr = postTool(t, s, "/mcp?caller=res-1", "task_inbox", `{`+cwd+`}`)
	testutil.False(t, isErr)
	testutil.Equal(t, store.inboxReads, 1)

	_, isErr = postTool(t, s, "/mcp?caller=res-1", "task_set_result", `{`+cwd+`,"result":{"ok":true}}`)
	testutil.False(t, isErr)
	testutil.Contains(t, rdb.resident.Result, "ok")
}

// An ordinary task naming its own cwd is untouched by the guard.
func TestResidentIdentityGuard_OrdinaryTaskUnaffected(t *testing.T) {
	s, _ := residentServer(t)
	store := &countingStore{mockMessageStore: &mockMessageStore{}}
	s.SetMessageManager(store, &mockNudger{})
	_, isErr := postTool(t, s, "/mcp", "task_inbox", `{"id":"abc123"}`)
	testutil.False(t, isErr)
	testutil.Equal(t, store.inboxReads, 1)
}
