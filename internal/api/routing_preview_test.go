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
	"github.com/argus-foundry/argus/internal/testutil"
)

func postPreview(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/routing/preview", body))
	return w
}

func TestHandleRoutingPreview(t *testing.T) {
	dir := t.TempDir()
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName),
		[]byte("[backends.cx]\ncommand = \"codex\"\nmodels = [\"m1\"]\n"), 0o644))
	d, err := db.Open(filepath.Join(dir, "data.sql"))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	srv := New(d, agent.NewRunner(nil), "test-token", nil, nil)
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetBackend("cx", config.Backend{Command: "codex"}))
	testutil.NoError(t, d.SetConfigValue("defaults.backend", "cl"))
	testutil.NoError(t, d.SetProject("plain", config.Project{Path: t.TempDir()}))
	testutil.NoError(t, d.SetProject("pinned", config.Project{Path: t.TempDir(), Backend: "cx"}))

	t.Run("inherited default", func(t *testing.T) {
		w := postPreview(t, srv, `{"project":"plain"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		var got map[string]any
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got["backend"], "cl")
		testutil.Equal(t, got["kind"], "claude")
		testutil.Equal(t, got["model_policy"], "curated")
		testutil.Equal(t, got["advisory"], true)
		testutil.True(t, got["default_model"] == nil)
		testutil.True(t, strings.Contains(w.Body.String(), `"default_model":null`))
	})
	t.Run("project override with configured models", func(t *testing.T) {
		w := postPreview(t, srv, `{"project":"pinned"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		var got routingPreviewResp
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got.Backend, "cx")
		testutil.Equal(t, got.ModelPolicy, "configured")
		testutil.DeepEqual(t, got.Models, []string{"m1"})
	})
	t.Run("explicit backend", func(t *testing.T) {
		w := postPreview(t, srv, `{"project":"pinned","backend":"cl"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		var got routingPreviewResp
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got.Backend, "cl")
	})
	t.Run("no candidate", func(t *testing.T) {
		w := postPreview(t, srv, `{"project":"plain","backend":"ghost"}`)
		testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
		testutil.True(t, strings.Contains(w.Body.String(), `"code":"backend_unavailable"`))
	})
	t.Run("unknown project", func(t *testing.T) {
		w := postPreview(t, srv, `{"project":"nope"}`)
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.True(t, strings.Contains(w.Body.String(), `"code":"project_not_found"`))
	})
	t.Run("malformed", func(t *testing.T) {
		for _, body := range []string{`{`, `{}`} {
			w := postPreview(t, srv, body)
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.True(t, strings.Contains(w.Body.String(), `"code":"invalid_request"`))
		}
	})
	t.Run("no side effects or account data", func(t *testing.T) {
		w := postPreview(t, srv, `{"project":"plain"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.False(t, strings.Contains(w.Body.String(), "account"))
		tasks, err := d.Tasks()
		testutil.NoError(t, err)
		testutil.Equal(t, len(tasks), 0)
	})
}
