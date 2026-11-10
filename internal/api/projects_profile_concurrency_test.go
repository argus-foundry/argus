package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestProjectProfile_OverlappingSaves(t *testing.T) {
	for _, tc := range []struct {
		name, profile string
		local         bool
	}{
		{"explicit profile", "default", false}, {"explicit clear", "", false}, {"local settings", "default", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, d := testServer(t)
			testutil.NoError(t, d.SetProject("p", config.Project{Path: "/repo", Profile: "lean"}))
			h := authMiddleware(srv.token, d, nil, srv.routes())
			srv.beforeProjectSave = func() {
				srv.beforeProjectSave = nil
				if tc.local {
					testutil.NoError(t, d.SetProject("p", config.Project{Path: "/repo", Profile: tc.profile}))
				} else {
					body, _ := json.Marshal(map[string]string{"path": "/repo", "profile": tc.profile})
					testutil.Equal(t, do(h, "PUT", "/api/projects/p", string(body)).Code, http.StatusOK)
				}
			}
			testutil.Equal(t, do(h, "PUT", "/api/projects/p", `{"path":"/renamed"}`).Code, http.StatusOK)
			stored, err := d.Projects()
			testutil.NoError(t, err)
			testutil.Equal(t, stored["p"].Profile, tc.profile)
			testutil.Equal(t, stored["p"].Path, "/renamed")
		})
	}
}

func TestProjectProfile_ConfigOnlyConflict(t *testing.T) {
	srv, _ := testServer(t)
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "data.sql"))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName), []byte("[projects.onlyfile]\npath = \"/repo\"\n"), 0600))
	srv.db = d
	h := authMiddleware(srv.token, d, nil, srv.routes())
	testutil.True(t, d.ProjectInConfigToml("onlyfile"))
	testutil.Equal(t, do(h, "PUT", "/api/projects/onlyfile", `{"path":"/renamed"}`).Code, http.StatusConflict)
	stored, err := d.Projects()
	testutil.NoError(t, err)
	_, exists := stored["onlyfile"]
	testutil.Equal(t, exists, false)
}

func TestProjectProfile_IdenticalPostEmitsNoEvent(t *testing.T) {
	h, d, sink := profileTestEnv(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: "/repo", Profile: "lean"}))
	testutil.Equal(t, do(h, "POST", "/api/projects", `{"name":"p","path":"/repo","profile":"lean"}`).Code, http.StatusCreated)
	testutil.Equal(t, len(sink.events()), 0)
	testutil.Equal(t, do(h, "POST", "/api/projects", `{"name":"p","path":"/changed","profile":"lean"}`).Code, http.StatusCreated)
	testutil.Equal(t, len(sink.events()), 1)
	testutil.Equal(t, sink.events()[0].Type, "config.changed")
}

func TestProjectProfile_OverlappingNoopEmitsNoEvent(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: "/repo", Profile: "lean"}))
	sink := &recordingSink{}
	previous := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(previous) })
	h := authMiddleware(srv.token, d, nil, srv.routes())
	srv.beforeProjectSave = func() {
		srv.beforeProjectSave = nil
		testutil.Equal(t, do(h, "PUT", "/api/projects/p", `{"path":"/renamed","profile":"default"}`).Code, http.StatusOK)
	}
	testutil.Equal(t, do(h, "PUT", "/api/projects/p", `{"path":"/renamed"}`).Code, http.StatusOK)
	testutil.Equal(t, len(sink.events()), 1)
}
