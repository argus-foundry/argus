package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func wtRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "master")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	runGit(t, repo, "config", "commit.gpgsign", "false")
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x\n"), 0o600))
	testutil.NoError(t, os.MkdirAll(filepath.Join(repo, "sub"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "sub", "g.txt"), []byte("g\n"), 0o600))
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

type wtRecorder struct{ ch chan model.Event }

// Hub-only tests own their repositories and can run in parallel. Tests using
// testServer must stay serial because that fixture changes HOME.
func newTestHub(t *testing.T) (*wtHub, *wtRecorder) {
	t.Helper()
	rec := &wtRecorder{ch: make(chan model.Event, 64)}
	h := newWtHub(func(ev model.Event) { rec.ch <- ev })
	h.debounce = 30 * time.Millisecond
	t.Cleanup(h.closeAll)
	return h, rec
}

func (r *wtRecorder) next(t *testing.T) (string, map[string]any) {
	t.Helper()
	select {
	case ev := <-r.ch:
		testutil.Equal(t, ev.Type, model.EventTypeWorktreeChanged)
		var p map[string]any
		testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
		return ev.TaskID, p
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worktree.changed")
		return "", nil
	}
}

func (r *wtRecorder) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case ev := <-r.ch:
		t.Fatalf("unexpected event %s %s", ev.Type, ev.Payload)
	case <-time.After(d):
	}
}

func TestWorktreeWatcher(t *testing.T) {
	t.Parallel()
	t.Run("agent and external edits publish a changed revision", func(t *testing.T) {
		repo := wtRepo(t)
		h, rec := newTestHub(t)
		snap, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.Equal(t, snap.stale, false)
		first := snap.revision

		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edited\n"), 0o600))
		id, p := rec.next(t)
		testutil.Equal(t, id, "t1")
		testutil.Equal(t, p["reason"], "filesystem")
		testutil.Equal(t, p["revision"] != first, true)

		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "sub", "g.txt"), []byte("ext\n"), 0o600))
		_, p = rec.next(t)
		testutil.Equal(t, p["reason"], "filesystem")

		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "sub", "new.txt"), []byte("n\n"), 0o600))
		_, _ = rec.next(t)
	})
	t.Run("new directories are watched", func(t *testing.T) {
		repo := wtRepo(t)
		h, rec := newTestHub(t)
		_, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.NoError(t, os.MkdirAll(filepath.Join(repo, "deep", "er"), 0o755))
		time.Sleep(200 * time.Millisecond)
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "deep", "er", "x.txt"), []byte("x"), 0o600))
		_, _ = rec.next(t)
	})
	t.Run("index change reports git reason", func(t *testing.T) {
		repo := wtRepo(t)
		h, rec := newTestHub(t)
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edited\n"), 0o600))
		_, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		runGit(t, repo, "add", "f.txt")
		_, p := rec.next(t)
		testutil.Equal(t, p["reason"], "git")
	})
	t.Run("HEAD checkout publishes", func(t *testing.T) {
		repo := wtRepo(t)
		runGit(t, repo, "checkout", "-q", "-b", "feat")
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "only-feat.txt"), []byte("f\n"), 0o600))
		runGit(t, repo, "add", ".")
		runGit(t, repo, "commit", "-q", "-m", "feat")
		h, rec := newTestHub(t)
		snap, err := h.status(context.Background(), "t1", repo, gitutil.Options{BaseBranch: "master"})
		testutil.NoError(t, err)
		runGit(t, repo, "checkout", "-q", "master")
		_, p := rec.next(t)
		testutil.Equal(t, p["revision"] != snap.revision, true)
	})
	t.Run("bursts coalesce and unchanged status emits nothing", func(t *testing.T) {
		repo := wtRepo(t)
		h, rec := newTestHub(t)
		var fetches atomic.Int32
		real := h.fetch
		h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			fetches.Add(1)
			return real(id, wt, o)
		}
		_, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		base := fetches.Load()
		for i := 0; i < 20; i++ {
			testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("burst\n"), 0o600))
		}
		_, last := rec.next(t)
		for quiet := false; !quiet; {
			select {
			case ev := <-rec.ch:
				var p map[string]any
				testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
				last = p
			case <-time.After(500 * time.Millisecond):
				quiet = true
			}
		}
		testutil.Equal(t, fetches.Load()-base < 20, true)
		final, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.Equal(t, last["revision"].(string), final.revision)
		testutil.Equal(t, final.stale, false)

		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "ignored-noop"), nil, 0o600))
		_, _ = rec.next(t)
		testutil.NoError(t, os.Remove(filepath.Join(repo, "ignored-noop")))
		_, _ = rec.next(t)
		testutil.NoError(t, os.Chtimes(filepath.Join(repo, "f.txt"), time.Now(), time.Now()))
		rec.none(t, 300*time.Millisecond)
	})
}

func TestWtHubStatus(t *testing.T) {
	t.Parallel()
	t.Run("cold then cached then stale", func(t *testing.T) {
		repo := wtRepo(t)
		h, _ := newTestHub(t)
		var fetches atomic.Int32
		real := h.fetch
		h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			fetches.Add(1)
			return real(id, wt, o)
		}
		h.maxAge = time.Hour
		s1, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		<-h.entries["t1"].watchReady
		<-h.entries["t1"].refresh("")
		base := fetches.Load()
		s2, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.Equal(t, s2.revision, s1.revision)
		testutil.Equal(t, fetches.Load(), base)

		h.maxAge = 0
		s3, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.Equal(t, s3.stale, true)
	})
	t.Run("collection error on cold read", func(t *testing.T) {
		h, _ := newTestHub(t)
		h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			return gitutil.GitStatusRefreshMsg{TaskID: id, Err: errors.New("boom")}
		}
		_, err := h.status(context.Background(), "t1", t.TempDir(), gitutil.Options{})
		testutil.Equal(t, err != nil && !errors.Is(err, errWtUnavailable), true)
	})
	t.Run("error after cache serves stale snapshot", func(t *testing.T) {
		repo := wtRepo(t)
		h, _ := newTestHub(t)
		_, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		<-h.entries["t1"].watchReady
		<-h.entries["t1"].refresh("")
		h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			return gitutil.GitStatusRefreshMsg{Err: errors.New("boom")}
		}
		<-h.entries["t1"].refresh("")
		s, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.Equal(t, s.stale, true)
	})
	t.Run("cold timeout", func(t *testing.T) {
		h, _ := newTestHub(t)
		h.coldTimeout = 20 * time.Millisecond
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			<-release
			return gitutil.GitStatusRefreshMsg{}
		}
		_, err := h.status(context.Background(), "t1", t.TempDir(), gitutil.Options{})
		testutil.ErrorIs(t, err, errWtUnavailable)
	})
	t.Run("cache is keyed on comparison base", func(t *testing.T) {
		repo := wtRepo(t)
		h, _ := newTestHub(t)
		a, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		b, err := h.status(context.Background(), "t1", repo, gitutil.Options{BaseBranch: "master"})
		testutil.NoError(t, err)
		testutil.Equal(t, a.msg.Comparison.RequestedBase, "")
		testutil.Equal(t, b.msg.Comparison.RequestedBase, "master")
		testutil.Equal(t, h.count(), 1)
	})
}

func TestWtHubLifecycle(t *testing.T) {
	t.Run("remove and sweep unregister", func(t *testing.T) {
		repo := wtRepo(t)
		h, _ := newTestHub(t)
		_, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		_, err = h.status(context.Background(), "t2", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.Equal(t, h.count(), 2)
		h.sweep(func(id, wt string) bool { return id == "t1" })
		testutil.Equal(t, h.count(), 1)
		h.remove("t1")
		testutil.Equal(t, h.count(), 0)
		h.remove("t1")
	})
	t.Run("sweep drops missing worktree", func(t *testing.T) {
		repo := wtRepo(t)
		h, _ := newTestHub(t)
		_, err := h.status(context.Background(), "t1", repo, gitutil.Options{})
		testutil.NoError(t, err)
		testutil.NoError(t, os.RemoveAll(repo))
		h.sweep(func(string, string) bool { return true })
		testutil.Equal(t, h.count(), 0)
	})
	t.Run("task.deleted event unregisters via server", func(t *testing.T) {
		srv, d := testServer(t)
		repo := wtRepo(t)
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
		testutil.NoError(t, d.Add(task))
		mux := srv.routes()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, srv.wt.count(), 1)
		srv.Emit(model.Event{Type: model.EventTypeTaskDeleted, TaskID: task.ID, At: time.Now()})
		testutil.Equal(t, srv.wt.count(), 0)
	})
	t.Run("event replay converges via the ring", func(t *testing.T) {
		srv, d := testServer(t)
		repo := wtRepo(t)
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
		testutil.NoError(t, d.Add(task))
		srv.wt.debounce = 30 * time.Millisecond
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		ch, cancel := srv.eventBus.subscribe()
		defer cancel()
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("z\n"), 0o600))
		select {
		case ev := <-ch:
			testutil.Equal(t, ev.Type, model.EventTypeWorktreeChanged)
			testutil.Equal(t, ev.ID > 0, true)
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
		}
		w = httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		var got struct {
			GitRevision string `json:"git_revision"`
			Stale       bool   `json:"stale"`
			Status      string `json:"Status"`
		}
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got.GitRevision != "", true)
		testutil.Equal(t, got.Status, " M f.txt")
	})
}

func TestHandleGitStatusErrors(t *testing.T) {
	t.Run("503 on cold timeout", func(t *testing.T) {
		srv, d := testServer(t)
		srv.wt.coldTimeout = 20 * time.Millisecond
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		srv.wt.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			<-release
			return gitutil.GitStatusRefreshMsg{}
		}
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: t.TempDir()}
		testutil.NoError(t, d.Add(task))
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
		var body map[string]string
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		testutil.Equal(t, body["code"], "git_status_unavailable")
	})
	t.Run("500 on collection error", func(t *testing.T) {
		srv, d := testServer(t)
		srv.wt.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			return gitutil.GitStatusRefreshMsg{Err: errors.New("boom")}
		}
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: t.TempDir()}
		testutil.NoError(t, d.Add(task))
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
	})
}

func discardReq(srv *Server, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+id+"/git/discard", body))
	return w
}

func TestHandleGitDiscard(t *testing.T) {
	setup := func(t *testing.T) (*Server, string, string, chan model.Event) {
		srv, d := testServer(t)
		srv.wt.debounce = 30 * time.Millisecond
		repo := wtRepo(t)
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
		testutil.NoError(t, d.Add(task))
		ch, cancel := srv.eventBus.subscribe()
		t.Cleanup(cancel)
		return srv, task.ID, repo, ch
	}
	t.Run("restores one file, emits discard event with revision after", func(t *testing.T) {
		srv, id, repo, ch := setup(t)
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edit\n"), 0o600))
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "sub", "g.txt"), []byte("keep\n"), 0o600))
		w := discardReq(srv, id, `{"path":"f.txt","scope":"local"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, w.Body.String(), `{"discarded":true,"path":"f.txt","scope":"local"}`+"\n")
		b, _ := os.ReadFile(filepath.Join(repo, "f.txt"))
		testutil.Equal(t, string(b), "x\n")
		b, _ = os.ReadFile(filepath.Join(repo, "sub", "g.txt"))
		testutil.Equal(t, string(b), "keep\n")

		var discardRevision any
		var found bool
		deadline := time.After(5 * time.Second)
		for !found {
			select {
			case ev := <-ch:
				var p map[string]any
				testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
				if p["reason"] == "discard" {
					found = true
					discardRevision = p["revision"]
					testutil.Equal(t, p["revision"] != nil, true)
					testutil.DeepEqual[any](t, p["paths"], []any{"f.txt"})
					testutil.Equal(t, ev.TaskID, id)
				}
			case <-deadline:
				t.Fatal("no discard event")
			}
		}
		g := httptest.NewRecorder()
		srv.routes().ServeHTTP(g, authedReq("GET", "/api/tasks/"+id+"/git/status", ""))
		var st struct {
			Status      string
			GitRevision string `json:"git_revision"`
		}
		testutil.NoError(t, json.Unmarshal(g.Body.Bytes(), &st))
		testutil.Equal(t, st.Status, " M sub/g.txt")
		testutil.Equal[any](t, discardRevision, st.GitRevision)
	})
	t.Run("untracked requires delete_untracked", func(t *testing.T) {
		srv, id, repo, _ := setup(t)
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, "n.txt"), []byte("n"), 0o600))
		w := discardReq(srv, id, `{"path":"n.txt"}`)
		testutil.Equal(t, w.Code, http.StatusConflict)
		w = discardReq(srv, id, `{"path":"n.txt","delete_untracked":true}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		_, err := os.Stat(filepath.Join(repo, "n.txt"))
		testutil.Equal(t, os.IsNotExist(err), true)
	})
	t.Run("rejections carry codes and emit nothing", func(t *testing.T) {
		srv, id, repo, ch := setup(t)
		outside := t.TempDir()
		testutil.NoError(t, os.WriteFile(filepath.Join(outside, "s"), []byte("s"), 0o600))
		testutil.NoError(t, os.Symlink(outside, filepath.Join(repo, "ln")))
		gitDirMv := func() { runGit(t, repo, "mv", "f.txt", "r.txt") }
		cases := []struct {
			name, body string
			status     int
			code       string
			pre        func()
		}{
			{"empty", `{"path":""}`, 400, "invalid_path", nil},
			{"absolute", `{"path":"/etc/passwd"}`, 400, "invalid_path", nil},
			{"traversal", `{"path":"../x"}`, 400, "invalid_path", nil},
			{"git meta", `{"path":".git/HEAD"}`, 400, "invalid_path", nil},
			{"directory", `{"path":"sub"}`, 400, "invalid_path", nil},
			{"symlink escape", `{"path":"ln/s","delete_untracked":true}`, 400, "invalid_path", nil},
			{"bad scope", `{"path":"f.txt","scope":"all"}`, 400, "invalid_scope", nil},
			{"bad json", `{`, 400, "invalid_request", nil},
			{"no changes", `{"path":"f.txt"}`, 409, "no_changes", nil},
			{"rename", `{"path":"f.txt"}`, 409, "rename_requires_explicit_paths", gitDirMv},
		}
		for _, c := range cases {
			if c.pre != nil {
				c.pre()
			}
			w := discardReq(srv, id, c.body)
			testutil.Equal(t, w.Code, c.status)
			var body map[string]string
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			testutil.Equal(t, body["code"], c.code)
		}
		b, _ := os.ReadFile(filepath.Join(outside, "s"))
		testutil.Equal(t, string(b), "s")
		for {
			select {
			case ev := <-ch:
				var p map[string]any
				_ = json.Unmarshal(ev.Payload, &p)
				testutil.Equal(t, p["reason"] != "discard", true)
				continue
			case <-time.After(300 * time.Millisecond):
			}
			break
		}
	})
	t.Run("404 for unknown task or missing worktree", func(t *testing.T) {
		srv, d := testServer(t)
		w := discardReq(srv, "nope", `{"path":"a"}`)
		testutil.Equal(t, w.Code, http.StatusNotFound)
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: filepath.Join(t.TempDir(), "gone")}
		testutil.NoError(t, d.Add(task))
		w = discardReq(srv, task.ID, `{"path":"a"}`)
		testutil.Equal(t, w.Code, http.StatusNotFound)
	})
}

func TestGitStatusFileCounts(t *testing.T) {
	srv, d := testServer(t)
	srv.wt.debounce = 30 * time.Millisecond
	repo := wtRepo(t)
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
	testutil.NoError(t, d.Add(task))
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x\ny\nz\n"), 0o600))
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "new.txt"), []byte("n\n"), 0o600))

	get := func() (rev string, files []gitutil.FileChange, status string) {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		testutil.Equal(t, w.Code, http.StatusOK)
		var got struct {
			GitRevision string               `json:"git_revision"`
			Files       []gitutil.FileChange `json:"files"`
			Status      string
		}
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		return got.GitRevision, got.Files, got.Status
	}
	rev, files, status := get()
	testutil.Contains(t, status, "f.txt")
	testutil.Equal(t, len(files), 2)
	m := map[string]gitutil.FileChange{}
	for _, f := range files {
		m[f.Path] = f
	}
	testutil.Equal(t, *m["f.txt"].Additions, 2)
	testutil.Equal(t, *m["f.txt"].Deletions, 0)
	testutil.Equal(t, *m["new.txt"].Additions, 1)

	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "new.txt"), []byte("n\nm\n"), 0o600))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rev2, files2, _ := get()
		if rev2 != rev {
			found := false
			for _, f := range files2 {
				if f.Path == "new.txt" {
					found = true
					testutil.Equal(t, f.Kind, gitutil.FileKindText)
					if f.Additions == nil || f.Deletions == nil {
						t.Fatal("updated counts missing")
					}
					testutil.Equal(t, *f.Additions, 2)
					testutil.Equal(t, *f.Deletions, 0)
				}
			}
			testutil.Equal(t, found, true)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("revision did not move with counts")
}
