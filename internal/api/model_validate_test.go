package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandleBackends_ModelPolicy(t *testing.T) {
	dir := t.TempDir()
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName),
		[]byte("[backends.cfgd]\ncommand = \"claude\"\nmodels = [\"only-this\"]\n"), 0o644))
	d, err := db.Open(filepath.Join(dir, "data.sql"))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	srv := New(d, agent.NewRunner(nil), "test-token", nil, nil)
	testutil.NoError(t, d.SetBackend("cur", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetBackend("cfgd", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetBackend("free", config.Backend{Command: "my-wrapper"}))

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/backends", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var resp struct {
		Backends []backendJSON `json:"backends"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	got := map[string]backendJSON{}
	for _, b := range resp.Backends {
		got[b.Name] = b
	}
	testutil.Equal(t, got["cur"].ModelPolicy, agent.ModelPolicyCurated)
	testutil.Equal(t, got["cfgd"].ModelPolicy, agent.ModelPolicyConfigured)
	testutil.DeepEqual(t, got["cfgd"].Models, []string{"only-this"})
	testutil.Equal(t, got["free"].ModelPolicy, agent.ModelPolicyFreeform)
	testutil.True(t, strings.Contains(w.Body.String(), `"model_policy":"freeform"`))
}

type modelRejection struct {
	Error          string   `json:"error"`
	Code           string   `json:"code"`
	Backend        string   `json:"backend"`
	RequestedModel string   `json:"requested_model"`
	AllowedModels  []string `json:"allowed_models"`
}

func TestHandleCreateTask_ModelNotAllowed(t *testing.T) {
	srv, _ := testServer(t)
	srv.createTask = func(name, prompt, project, backend, taskModel, sandboxOverride, account string, _ bool) (*model.Task, error) {
		return nil, &agent.ModelNotAllowedError{Backend: "claude", RequestedModel: taskModel, AllowedModels: []string{"opus"}}
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks", `{"project":"p","prompt":"work","model":"gpt-9"}`))
	testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
	var got modelRejection
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Code, "model_not_allowed")
	testutil.Equal(t, got.Error, "model is not allowed for resolved backend")
	testutil.Equal(t, got.Backend, "claude")
	testutil.Equal(t, got.RequestedModel, "gpt-9")
	testutil.DeepEqual(t, got.AllowedModels, []string{"opus"})
}

func TestHandleCreateTask_Multipart_ModelNotAllowed(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: filepath.Join(t.TempDir(), "missing"), Branch: "HEAD"}))

	ct, body := buildMultipart(t,
		[][3]string{{"name", "x", ""}, {"prompt", "p", ""}, {"project", "proj", ""}, {"backend", "cl", ""}, {"model", "gpt-9", ""}},
		[][3]string{{"files", "a.txt", "a"}},
	)
	req := httptest.NewRequest("POST", "/api/tasks", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)

	testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
	var got modelRejection
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Code, "model_not_allowed")
	testutil.Equal(t, got.Backend, "cl")
	testutil.DeepEqual(t, got.AllowedModels, agent.KnownModels("claude"))
	tasks, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 0)
}

func TestHandleBackends_MergedCommandPolicy(t *testing.T) {
	for _, tc := range []struct{ name, stored, merged, policy string }{
		{"freeform to curated", "pi", "claude", agent.ModelPolicyCurated},
		{"curated to freeform", "claude", "pi", agent.ModelPolicyFreeform},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName), []byte("[backends.over]\ncommand = \""+tc.merged+"\"\n"), 0o600))
			d, err := db.Open(filepath.Join(dir, "data.sql"))
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			testutil.NoError(t, d.SetBackend("over", config.Backend{Command: tc.stored}))
			srv := New(d, agent.NewRunner(nil), "test-token", nil, nil)
			close(srv.stopCh)
			<-srv.idleWatcherDone
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("GET", "/api/backends", ""))
			testutil.Equal(t, w.Code, http.StatusOK)
			var resp struct {
				Backends []backendJSON `json:"backends"`
			}
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			found := false
			for _, b := range resp.Backends {
				if b.Name == "over" {
					found = true
					testutil.Equal(t, b.ModelPolicy, tc.policy)
					testutil.DeepEqual(t, b.Models, agent.BackendModels(d.Config().Backends["over"]))
				}
			}
			testutil.True(t, found)
		})
	}
}

func TestHandleCreateTask_JSON_ModelNotAllowedIntegration(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: filepath.Join(t.TempDir(), "missing"), Branch: "HEAD"}))
	calls := 0
	srv.createTask = func(name, prompt, project, backend, taskModel, sandboxOverride, account string, _ bool) (*model.Task, error) {
		calls++
		task, _, err := agent.CreateAndStart(d, srv.runner, agent.CreateInput{Name: name, Prompt: prompt, Project: project, Backend: backend, Model: taskModel, SandboxOverride: sandboxOverride, Account: account})
		return task, err
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks", `{"name":"json-reject","project":"proj","prompt":"p","backend":"cl","model":"gpt-9"}`))
	testutil.Equal(t, calls, 1)
	testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
	var got modelRejection
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Code, "model_not_allowed")
	testutil.Equal(t, got.Backend, "cl")
	testutil.Equal(t, got.RequestedModel, "gpt-9")
	testutil.DeepEqual(t, got.AllowedModels, agent.KnownModels("claude"))
	tasks, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 0)
	_, err = os.Stat(agent.WorktreeDir("proj", "json-reject"))
	testutil.True(t, os.IsNotExist(err))
	testutil.Equal(t, len(srv.runner.Running()), 0)
}
