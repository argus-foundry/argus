package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func profileTestEnv(t *testing.T) (http.Handler, *db.DB, *recordingSink) {
	t.Helper()
	srv, d := testServer(t)
	dir := filepath.Join(db.DataDir(), "profiles")
	testutil.NoError(t, os.MkdirAll(dir, 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "lean.toml"), []byte(""), 0o644))
	sink := &recordingSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	return authMiddleware(srv.token, d, nil, srv.routes()), d, sink
}

func do(h http.Handler, method, url, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(method, url, body))
	return w
}

func TestProjectProfile_PreservedAcrossUnrelatedEdit(t *testing.T) {
	h, d, sink := profileTestEnv(t)
	testutil.NoError(t, d.SetProject("argus", config.Project{Path: "/repo", Profile: "lean"}))

	w := do(h, "PUT", "/api/projects/argus", `{"path":"/repo2","backend":"codex","sandbox":{"enabled":true}}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	var resp projectJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testutil.Equal(t, *resp.Profile, "lean")

	stored, err := d.Projects()
	testutil.NoError(t, err)
	testutil.Equal(t, stored["argus"].Profile, "lean")
	testutil.Equal(t, stored["argus"].Path, "/repo2")
	testutil.Equal(t, stored["argus"].Backend, "codex")

	evs := sink.events()
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].Type, model.EventTypeConfigChanged)
	testutil.Equal(t, evs[0].TaskID, "")
	testutil.Equal(t, string(evs[0].Payload), `{"name":"argus","resource":"projects"}`)
}

func TestProjectProfile_GetSetClear(t *testing.T) {
	h, d, _ := profileTestEnv(t)
	w := do(h, "POST", "/api/projects", `{"name":"p","path":"/p","profile":"lean"}`)
	testutil.Equal(t, w.Code, http.StatusCreated)
	testutil.Contains(t, w.Body.String(), `"profile":"lean"`)

	w = do(h, "GET", "/api/projects/full", "")
	testutil.Contains(t, w.Body.String(), `"profile":"lean"`)

	w = do(h, "PUT", "/api/projects/p", `{"path":"/p","profile":""}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), `"profile":""`)
	stored, _ := d.Projects()
	testutil.Equal(t, stored["p"].Profile, "")

	w = do(h, "PUT", "/api/projects/p", `{"path":"/p","profile":"lean"}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	stored, _ = d.Projects()
	testutil.Equal(t, stored["p"].Profile, "lean")

	testutil.NoError(t, d.SetProject("q", config.Project{Path: "/q"}))
	w = do(h, "GET", "/api/projects/full", "")
	testutil.Contains(t, w.Body.String(), `"name":"q","path":"/q","profile":""`)
}

func TestProjectProfile_Errors(t *testing.T) {
	h, d, sink := profileTestEnv(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: "/p", Profile: "lean"}))
	for _, tc := range []struct {
		name, method, url, body string
		want                    int
	}{
		{"bad json", "PUT", "/api/projects/p", `nope`, 400},
		{"blank path", "PUT", "/api/projects/p", `{"path":" "}`, 400},
		{"unknown profile put", "PUT", "/api/projects/p", `{"path":"/p","profile":"ghost"}`, 400},
		{"unknown profile post", "POST", "/api/projects", `{"name":"n","path":"/n","profile":"ghost"}`, 400},
		{"unknown project", "PUT", "/api/projects/missing", `{"path":"/m"}`, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, do(h, tc.method, tc.url, tc.body).Code, tc.want)
		})
	}
	stored, _ := d.Projects()
	testutil.Equal(t, stored["p"].Profile, "lean")
	testutil.Equal(t, len(sink.events()), 0)

	req := httptest.NewRequest("PUT", "/api/projects/p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	testutil.Equal(t, w.Code, http.StatusUnauthorized)
}

func TestProjectProfile_NoOpEmitsNoEvent(t *testing.T) {
	h, d, sink := profileTestEnv(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: "/p", Profile: "lean"}))
	testutil.Equal(t, do(h, "PUT", "/api/projects/p", `{"path":"/p"}`).Code, http.StatusOK)
	testutil.Equal(t, len(sink.events()), 0)
}

func TestProjectProfile_ConfigTomlOwnedConflict(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "data.sql"))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, d.SetProject("owned", config.Project{Path: "/o"}))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName), []byte("[projects.owned]\npath = \"/o\"\n"), 0o644))

	srv, _ := testServer(t)
	srv.db = d
	srv.runner = agent.NewRunner(nil)
	h := authMiddleware(srv.token, d, nil, srv.routes())
	sink := &recordingSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })

	testutil.Equal(t, do(h, "PUT", "/api/projects/owned", `{"path":"/x","profile":"default"}`).Code, http.StatusConflict)
	testutil.Equal(t, do(h, "POST", "/api/projects", `{"name":"owned","path":"/x"}`).Code, http.StatusConflict)
	stored, _ := d.Projects()
	testutil.Equal(t, stored["owned"].Path, "/o")
	testutil.Equal(t, len(sink.events()), 0)
}

func TestProjectProfile_StoreFailure(t *testing.T) {
	h, d, sink := profileTestEnv(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: "/p"}))
	testutil.NoError(t, d.Close())
	testutil.Equal(t, do(h, "PUT", "/api/projects/p", `{"path":"/p2"}`).Code, http.StatusInternalServerError)
	testutil.Equal(t, len(sink.events()), 0)
}
