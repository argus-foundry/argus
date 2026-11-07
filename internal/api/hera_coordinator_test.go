package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

const (
	coordReq1 = "11111111-1111-4111-8111-111111111111"
	coordReq2 = "22222222-2222-4222-8222-222222222222"
)

type eventCollector struct {
	mu  sync.Mutex
	evs []model.Event
}

func (c *eventCollector) Emit(ev model.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evs = append(c.evs, ev)
}

func (c *eventCollector) ofType(typ string) []model.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []model.Event
	for _, e := range c.evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func coordGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func coordRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	coordGit(t, repo, "init", "-q")
	coordGit(t, repo, "config", "user.email", "t@t.com")
	coordGit(t, repo, "config", "user.name", "T")
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi"), 0o644))
	coordGit(t, repo, "add", ".")
	coordGit(t, repo, "commit", "-q", "-m", "init")
	head := strings.TrimSpace(coordGit(t, repo, "rev-parse", "HEAD"))
	coordGit(t, repo, "update-ref", "refs/remotes/origin/master", head)
	coordGit(t, repo, "update-ref", "refs/remotes/origin/feature", head)
	coordGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/master")
	return repo
}

func coordServer(t *testing.T) (*Server, *db.DB, string, *eventCollector) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	repo := coordRepo(t)
	srv, d := testServer(t)
	testutil.NoError(t, d.SetConfigValue("defaults.backend", "test"))
	testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "echo hello"}))
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "origin/master"}))
	profDir := filepath.Join(db.DataDir(), "profiles")
	testutil.NoError(t, os.MkdirAll(profDir, 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(profDir, "lean.toml"), []byte("[archetype.orchestrator]\neffort = \"low\"\n[archetype.orchestrator.models]\nclaude = \"opus\"\n"), 0o644))
	col := &eventCollector{}
	prev := events.SetSink(col)
	t.Cleanup(func() { events.SetSink(prev) })
	return srv, d, repo, col
}

func coordPost(srv *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, authedReq("POST", "/api/hera/orchestrators", body))
	return rec
}

func coordBody(id string, extra string) string {
	b := `{"request_id":"` + id + `","project":"proj","prompt":"Coordinate this work"`
	if extra != "" {
		b += "," + extra
	}
	return b + "}"
}

func TestHeraCoordinatorOptions(t *testing.T) {
	srv, _, _, _ := coordServer(t)
	mux := srv.routes()

	t.Run("choices and defaults", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedReq("GET", "/api/hera/coordinator-options?project=proj", ""))
		testutil.Equal(t, rec.Code, http.StatusOK)
		var got heraCoordOptions
		testutil.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		testutil.Equal(t, got.Defaults.BaseBranch, "origin/master")
		testutil.Equal(t, got.Defaults.Profile, "default")
		testutil.Equal(t, got.Defaults.Archetype, "orchestrator")
		testutil.Contains(t, strings.Join(got.Branches, ","), "origin/feature")
		testutil.Equal(t, got.Branches[0], "origin/master")
		testutil.Contains(t, strings.Join(got.Profiles, ","), "lean")
		testutil.Contains(t, strings.Join(got.Archetypes, ","), "orchestrator")
	})
	t.Run("unknown project 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedReq("GET", "/api/hera/coordinator-options?project=nope", ""))
		testutil.Equal(t, rec.Code, http.StatusNotFound)
	})
	t.Run("missing project 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedReq("GET", "/api/hera/coordinator-options", ""))
		testutil.Equal(t, rec.Code, http.StatusNotFound)
	})
	t.Run("unauthenticated 401", func(t *testing.T) {
		hs := httptest.NewServer(authMiddleware(srv.token, srv.db, srv.push, srv.routes(), "/"))
		defer hs.Close()
		resp, err := http.Get(hs.URL + "/api/hera/coordinator-options?project=proj")
		testutil.NoError(t, err)
		defer resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusUnauthorized)
		resp2, err := http.Post(hs.URL+"/api/hera/orchestrators", "application/json", strings.NewReader(coordBody(coordReq1, "")))
		testutil.NoError(t, err)
		defer resp2.Body.Close()
		testutil.Equal(t, resp2.StatusCode, http.StatusUnauthorized)
	})
}

func TestHeraCoordinatorCreate_Success(t *testing.T) {
	srv, d, _, col := coordServer(t)

	rec := coordPost(srv, coordBody(coordReq1, `"name":"delivery","base_branch":"origin/feature","profile":"lean","archetype":"brainstorm"`))
	testutil.Equal(t, rec.Code, http.StatusCreated)
	var got heraCoordCreateResp
	testutil.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	testutil.Equal(t, got.Name, "delivery")
	testutil.Equal(t, got.BaseBranch, "origin/feature")
	testutil.Equal(t, got.Profile, "lean")
	testutil.Equal(t, got.Archetype, "brainstorm")

	task, err := d.Get(got.TaskID)
	testutil.NoError(t, err)
	testutil.Equal(t, task.Profile, "lean")
	testutil.Equal(t, task.Archetype, "brainstorm")
	testutil.Equal(t, task.Project, "proj")
	testutil.Equal(t, task.Branch, "argus/delivery/coord-delivery")

	roles, err := d.ListHeraRoles(got.OrchestratorID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(roles), 1)
	testutil.Equal(t, roles[0].ID, got.RoleID)
	testutil.Equal(t, roles[0].Archetype, "brainstorm")
	b, err := d.HeraLiveBindingByTaskAndOrchestrator(got.TaskID, got.OrchestratorID)
	testutil.NoError(t, err)
	testutil.Equal(t, b.ID, got.BindingID)

	evs := col.ofType(model.EventTypeHeraOrchestratorCreated)
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].TaskID, got.TaskID)
	var payload heraCoordCreatedEvent
	testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
	testutil.Equal(t, payload, heraCoordCreatedEvent{OrchestratorID: got.OrchestratorID, RoleID: got.RoleID, TaskID: got.TaskID})
}

func TestHeraCoordinatorCreate_Defaults(t *testing.T) {
	srv, d, _, _ := coordServer(t)

	rec := coordPost(srv, coordBody(coordReq1, ""))
	testutil.Equal(t, rec.Code, http.StatusCreated)
	var got heraCoordCreateResp
	testutil.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	testutil.Equal(t, got.BaseBranch, "origin/master")
	testutil.Equal(t, got.Profile, "default")
	testutil.Equal(t, got.Archetype, "orchestrator")
	task, err := d.Get(got.TaskID)
	testutil.NoError(t, err)
	testutil.Equal(t, task.Profile, "")
	testutil.Equal(t, task.Archetype, "orchestrator")
}

func TestHeraCoordinatorCreate_ModelResolution(t *testing.T) {
	srv, d, _, _ := coordServer(t)
	testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude"}))

	rec := coordPost(srv, coordBody(coordReq1, `"profile":"lean","backend":"claude"`))
	testutil.Equal(t, rec.Code, http.StatusCreated)
	var got heraCoordCreateResp
	testutil.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	task, err := d.Get(got.TaskID)
	testutil.NoError(t, err)
	cfg := d.Config()
	backend, err := agent.ResolveBackend(task, cfg)
	testutil.NoError(t, err)
	m, rp, overridden := agent.ResolveModelDetailed(task, backend, cfg)
	testutil.Equal(t, overridden, false)
	testutil.Equal(t, m, "opus")
	if rp == nil {
		t.Fatal("expected profile-resolved model")
	}
	testutil.Equal(t, rp.Name, "lean")
	testutil.Equal(t, rp.Archetype, "orchestrator")
}

func TestHeraCoordinatorCreate_Validation(t *testing.T) {
	srv, _, _, col := coordServer(t)
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"invalid json", `{`, 400},
		{"unknown field", coordBody(coordReq1, `"command":"rm -rf /"`), 400},
		{"worktree path rejected", coordBody(coordReq1, `"worktree":"/tmp/x"`), 400},
		{"bad uuid", coordBody("not-a-uuid", ""), 400},
		{"missing project", `{"request_id":"` + coordReq1 + `","prompt":"x"}`, 400},
		{"empty prompt", `{"request_id":"` + coordReq1 + `","project":"proj","prompt":"  "}`, 400},
		{"bad ref", coordBody(coordReq1, `"base_branch":"--evil"`), 400},
		{"missing ref", coordBody(coordReq1, `"base_branch":"origin/nope"`), 400},
		{"bad profile", coordBody(coordReq1, `"profile":"../../etc/passwd"`), 400},
		{"unknown profile", coordBody(coordReq1, `"profile":"ghost"`), 400},
		{"bad archetype", coordBody(coordReq1, `"archetype":"wizard"`), 400},
		{"bad backend", coordBody(coordReq1, `"backend":"nope"`), 400},
		{"unknown project", `{"request_id":"` + coordReq1 + `","project":"nope","prompt":"x"}`, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := coordPost(srv, tt.body)
			testutil.Equal(t, rec.Code, tt.status)
		})
	}
	testutil.Equal(t, len(col.ofType(model.EventTypeHeraOrchestratorCreated)), 0)
	orchs, err := srv.db.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(orchs), 0)
	// Validation failures never claim the receipt.
	rec := coordPost(srv, coordBody(coordReq1, ""))
	testutil.Equal(t, rec.Code, http.StatusCreated)
}

func TestHeraCoordinatorCreate_Idempotency(t *testing.T) {
	srv, d, _, col := coordServer(t)

	first := coordPost(srv, coordBody(coordReq1, `"name":"delivery"`))
	testutil.Equal(t, first.Code, http.StatusCreated)

	// Whitespace/key-order differences still count as the identical body.
	replay := coordPost(srv, `{"name":" delivery ","prompt":"Coordinate this work","project":"proj","request_id":"`+strings.ToUpper(coordReq1)+`"}`)
	testutil.Equal(t, replay.Code, http.StatusCreated)
	testutil.Equal(t, replay.Header().Get("Idempotent-Replay"), "true")
	testutil.Equal(t, strings.TrimSpace(replay.Body.String()), strings.TrimSpace(first.Body.String()))

	changed := coordPost(srv, coordBody(coordReq1, `"name":"other"`))
	testutil.Equal(t, changed.Code, http.StatusConflict)

	orchs, err := d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(orchs), 1)
	tasks, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 1)
	testutil.Equal(t, len(col.ofType(model.EventTypeHeraOrchestratorCreated)), 1)
}

func TestHeraCoordinatorCreate_ConcurrentSameRequest(t *testing.T) {
	srv, d, _, _ := coordServer(t)
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = coordPost(srv, coordBody(coordReq1, `"name":"delivery"`)).Code
		}(i)
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else {
			testutil.Equal(t, c, http.StatusConflict)
		}
	}
	if created < 1 {
		t.Fatalf("no request succeeded: %v", codes)
	}
	orchs, err := d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(orchs), 1)
	tasks, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 1)
}

func TestHeraCoordinatorCreate_ReplayAfterRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := coordRepo(t)
	path := filepath.Join(t.TempDir(), "data.sql")
	open := func() (*Server, *db.DB) {
		d, err := db.Open(path)
		testutil.NoError(t, err)
		srv := New(d, agent.NewRunner(nil), "test-token", nil, nil)
		return srv, d
	}
	srv, d := open()
	testutil.NoError(t, d.SetConfigValue("defaults.backend", "test"))
	testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "echo hello"}))
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "origin/master"}))
	first := coordPost(srv, coordBody(coordReq1, ""))
	testutil.Equal(t, first.Code, http.StatusCreated)
	testutil.NoError(t, d.Close())

	srv2, d2 := open()
	defer func() { _ = d2.Close() }()
	replay := coordPost(srv2, coordBody(coordReq1, ""))
	testutil.Equal(t, replay.Code, http.StatusCreated)
	testutil.Equal(t, strings.TrimSpace(replay.Body.String()), strings.TrimSpace(first.Body.String()))
	tasks, err := d2.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 1)
}

func TestHeraCoordinatorCreate_SpawnFailure500(t *testing.T) {
	srv, d, _, col := coordServer(t)
	calls := 0
	srv.spawnCoordinator = func(*db.DB, agent.SessionProvider, agent.HeraCoordinatorSpawnInput) (*agent.HeraCoordinatorSpawnResult, error) {
		calls++
		return nil, errors.New("boom")
	}
	rec := coordPost(srv, coordBody(coordReq1, ""))
	testutil.Equal(t, rec.Code, http.StatusInternalServerError)
	testutil.Equal(t, len(col.ofType(model.EventTypeHeraOrchestratorCreated)), 0)

	// The receipt was released, so the same request_id retries and spawns.
	srv.spawnCoordinator = nil
	rec = coordPost(srv, coordBody(coordReq1, ""))
	testutil.Equal(t, rec.Code, http.StatusCreated)
	testutil.Equal(t, calls, 1)
	orchs, err := d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(orchs), 1)
}

type coordStartFailRunner struct{ agent.SessionRunner }

func (coordStartFailRunner) Start(*model.Task, config.Config, uint16, uint16, bool) (agent.SessionHandle, error) {
	return nil, errors.New("start failed")
}

func TestHeraCoordinatorCreate_StartFailureLeavesNoOrphans(t *testing.T) {
	srv, d, repo, col := coordServer(t)
	srv.runner = coordStartFailRunner{srv.runner}

	rec := coordPost(srv, coordBody(coordReq2, `"name":"doomed"`))
	testutil.Equal(t, rec.Code, http.StatusInternalServerError)

	tasks, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 0)
	orchs, err := d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(orchs), 0)
	testutil.Equal(t, len(col.ofType(model.EventTypeHeraOrchestratorCreated)), 0)
	branches := coordGit(t, repo, "branch", "--list", "argus/*")
	testutil.Equal(t, strings.TrimSpace(branches), "")
	wts := coordGit(t, repo, "worktree", "list", "--porcelain")
	testutil.Equal(t, strings.Count(wts, "worktree "), 1)

	// Failure released the receipt: the same request_id can retry.
	srv.runner = agent.NewRunner(nil)
	rec = coordPost(srv, coordBody(coordReq2, `"name":"doomed"`))
	testutil.Equal(t, rec.Code, http.StatusCreated)
}
