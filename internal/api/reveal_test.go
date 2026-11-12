package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type launchCall struct {
	name string
	args []string
}

func revealFixture(t *testing.T) (*Server, http.Handler, string, string, *[]launchCall) {
	t.Helper()
	srv, d := testServer(t)
	wt := t.TempDir()
	if r, err := filepath.EvalSymlinks(wt); err == nil {
		wt = r
	}
	task := &model.Task{Name: "t", Worktree: wt, Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	var calls []launchCall
	srv.revealGOOS = "darwin"
	srv.revealLaunch = func(name string, args ...string) error {
		calls = append(calls, launchCall{name, args})
		return nil
	}
	return srv, authMiddleware(srv.token, d, nil, srv.routes()), task.ID, wt, &calls
}

func TestReveal_FileAndWorktree(t *testing.T) {
	_, h, id, wt, calls := revealFixture(t)
	testutil.NoError(t, os.MkdirAll(filepath.Join(wt, "sub"), 0o755))
	spaced := filepath.Join(wt, "sub", "my file  name.txt")
	testutil.NoError(t, os.WriteFile(spaced, []byte("x"), 0o644))

	t.Run("file keeps whitespace as a single argument", func(t *testing.T) {
		*calls = nil
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{"path":"sub/my file  name.txt"}`))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Contains(t, w.Body.String(), `"target":"file"`)
		testutil.Contains(t, w.Body.String(), `"host":"daemon"`)
		testutil.Equal(t, len(*calls), 1)
		testutil.Equal(t, (*calls)[0].name, "open")
		testutil.DeepEqual(t, (*calls)[0].args, []string{"-R", spaced})
	})
	t.Run("worktree with empty object", func(t *testing.T) {
		*calls = nil
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{}`))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Contains(t, w.Body.String(), `"target":"worktree"`)
		testutil.DeepEqual(t, (*calls)[0].args, []string{"-a", "Finder", wt})
	})
	t.Run("worktree with no body", func(t *testing.T) {
		*calls = nil
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", ""))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, len(*calls), 1)
	})
}

func TestReveal_RejectsWithoutLaunching(t *testing.T) {
	_, h, id, wt, calls := revealFixture(t)
	outside := t.TempDir()
	testutil.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o644))
	testutil.NoError(t, os.Symlink(outside, filepath.Join(wt, "escape")))
	testutil.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(wt, "link-file")))
	testutil.NoError(t, os.MkdirAll(filepath.Join(wt, ".git"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(wt, ".git", "config"), []byte("c"), 0o644))
	testutil.NoError(t, os.Symlink(filepath.Join(wt, ".git"), filepath.Join(wt, "gitlink")))
	testutil.NoError(t, os.MkdirAll(filepath.Join(wt, "dir"), 0o755))

	for _, tc := range []struct{ name, path string }{
		{"traversal", "../x"},
		{"nested traversal", "a/../../x"},
		{"absolute", filepath.Join(outside, "secret")},
		{"symlinked dir escape", "escape/secret"},
		{"symlinked file escape", "link-file"},
		{".git file", ".git/config"},
		{".git uppercase", ".GIT/config"},
		{".git via symlink", "gitlink/config"},
		{"directory not file", "dir"},
		{"nul byte", "a\u0000b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"path":` + jsonString(tc.path) + `}`
			w := httptest.NewRecorder()
			h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", body))
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.Contains(t, w.Body.String(), "invalid_path")
			testutil.Equal(t, len(*calls), 0)
		})
	}
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteString(string("0123456789abcdef"[r>>4]))
			b.WriteString(string("0123456789abcdef"[r&0xf]))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestReveal_ErrorResponses(t *testing.T) {
	t.Run("unknown task", func(t *testing.T) {
		_, h, _, _, calls := revealFixture(t)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/nope/reveal", `{}`))
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.Contains(t, w.Body.String(), "task_not_found")
		testutil.Equal(t, len(*calls), 0)
	})
	t.Run("missing path", func(t *testing.T) {
		_, h, id, _, calls := revealFixture(t)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{"path":"nope.txt"}`))
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.Contains(t, w.Body.String(), "path_not_found")
		testutil.Equal(t, len(*calls), 0)
	})
	t.Run("missing worktree dir", func(t *testing.T) {
		srv, h, id, wt, calls := revealFixture(t)
		_ = srv
		testutil.NoError(t, os.RemoveAll(wt))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{}`))
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.Contains(t, w.Body.String(), "path_not_found")
		testutil.Equal(t, len(*calls), 0)
	})
	t.Run("unsupported host", func(t *testing.T) {
		srv, h, id, _, calls := revealFixture(t)
		srv.revealGOOS = "linux"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{}`))
		testutil.Equal(t, w.Code, http.StatusNotImplemented)
		testutil.Contains(t, w.Body.String(), "unsupported_host")
		testutil.Equal(t, len(*calls), 0)
	})
	t.Run("launch failure", func(t *testing.T) {
		srv, h, id, _, _ := revealFixture(t)
		srv.revealLaunch = func(string, ...string) error { return os.ErrPermission }
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{}`))
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		testutil.Contains(t, w.Body.String(), "reveal_failed")
	})
	t.Run("malformed json", func(t *testing.T) {
		_, h, id, _, calls := revealFixture(t)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{bad`))
		testutil.Equal(t, w.Code, http.StatusBadRequest)
		testutil.Equal(t, len(*calls), 0)
	})
	t.Run("device token forbidden", func(t *testing.T) {
		srv, h, id, _, calls := revealFixture(t)
		plain, _, err := MintToken(srv.db, "phone")
		testutil.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/tasks/"+id+"/reveal", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+plain)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		testutil.Equal(t, w.Code, http.StatusForbidden)
		testutil.Equal(t, len(*calls), 0)
	})
	t.Run("unauthenticated", func(t *testing.T) {
		_, h, id, _, _ := revealFixture(t)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/"+id+"/reveal", strings.NewReader(`{}`)))
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	})
}

func TestExecRevealLauncher(t *testing.T) {
	testutil.NoError(t, execRevealLauncher("true"))
	if err := execRevealLauncher("false"); err == nil {
		t.Fatal("expected failure")
	}
}
