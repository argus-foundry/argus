package api

import (
	"context"
	"encoding/json"
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

func TestWtDiscardTimeout(t *testing.T) {
	t.Parallel()
	h, rec := newTestHub(t)
	repo := wtRepo(t)
	_, err := h.status(context.Background(), "t", repo, gitutil.Options{})
	testutil.NoError(t, err)
	<-h.entries["t"].watchReady
	<-h.entries["t"].refresh("")
	h.coldTimeout = 10 * time.Millisecond
	started, release := make(chan struct{}), make(chan struct{})
	h.fetch = func(string, string, gitutil.Options) gitutil.GitStatusRefreshMsg {
		close(started)
		<-release
		return gitutil.GitStatusRefreshMsg{}
	}
	h.publishDiscard("t", repo, gitutil.Options{}, []string{"f.txt"})
	<-started
	_, p := rec.next(t)
	e := h.entries["t"]
	e.mu.Lock()
	stale := e.snapshotLocked().stale
	done := e.inflight
	e.mu.Unlock()
	close(release)
	<-done
	testutil.Equal(t, p["revision"], nil)
	testutil.Equal(t, stale, true)
}

func TestWtArchivedRead(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: wtRepo(t)}
	testutil.NoError(t, d.Add(task))
	get := func() {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		testutil.Equal(t, w.Code, http.StatusOK)
	}
	get()
	task.Archived = true
	testutil.NoError(t, d.Update(task))
	srv.Emit(model.Event{Type: model.EventTypeTaskArchived, TaskID: task.ID, At: time.Now()})
	get()
	testutil.Equal(t, srv.wt.count(), 0)
	w := discardReq(srv, task.ID, `{"path":"f.txt"}`)
	assertDiscardBody(t, w, http.StatusConflict, "task_archived")
	testutil.Equal(t, srv.wt.count(), 0)
}

func TestWtSymlinkRoot(t *testing.T) {
	t.Parallel()
	repo := wtRepo(t)
	link := filepath.Join(t.TempDir(), "root")
	testutil.NoError(t, os.Symlink(repo, link))
	h, rec := newTestHub(t)
	_, err := h.status(context.Background(), "t", link, gitutil.Options{})
	testutil.NoError(t, err)
	<-h.entries["t"].watchReady
	<-h.entries["t"].refresh("")
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edited\n"), 0o600))
	_, p := rec.next(t)
	testutil.Equal(t, p["reason"], "filesystem")
}

func TestWtSameSummaryEdit(t *testing.T) {
	t.Parallel()
	repo := wtRepo(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("first\n"), 0o600))
	h, rec := newTestHub(t)
	first, err := h.status(context.Background(), "t", repo, gitutil.Options{})
	testutil.NoError(t, err)
	// Explicit timestamps avoid filesystem clock granularity assumptions.
	next := time.Now().Add(time.Second)
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("other\n"), 0o600))
	testutil.NoError(t, os.Chtimes(filepath.Join(repo, "f.txt"), next, next))
	<-h.entries["t"].refresh("filesystem")
	_, p := rec.next(t)
	testutil.Equal(t, p["revision"] != first.revision, true)
	final, err := h.status(context.Background(), "t", repo, gitutil.Options{})
	testutil.NoError(t, err)
	testutil.Equal(t, final.msg.Status, first.msg.Status)
	testutil.Equal(t, final.msg.Diff, first.msg.Diff)
}

func TestWtInflightFollowup(t *testing.T) {
	t.Parallel()
	h, rec := newTestHub(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls, active, maxActive atomic.Int32
	h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
		n := active.Add(1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		defer active.Add(-1)
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
			return gitutil.GitStatusRefreshMsg{Status: " M old"}
		}
		return gitutil.GitStatusRefreshMsg{Status: " M new"}
	}
	e := h.register("t", t.TempDir(), gitutil.Options{})
	done := e.refresh("filesystem")
	<-started
	_ = e.refresh("filesystem")
	close(release)
	<-done
	e.mu.Lock()
	status := e.msg.Status
	rev := e.revision
	e.mu.Unlock()
	testutil.Equal(t, calls.Load(), int32(2))
	testutil.Equal(t, maxActive.Load(), int32(1))
	testutil.Equal(t, status, " M new")
	_, p := rec.next(t)
	testutil.Equal[any](t, p["revision"], rev)
}

func assertDiscardBody(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	testutil.Equal(t, w.Code, status)
	var body map[string]string
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	testutil.Equal(t, body["code"], code)
	testutil.Equal(t, body["error"] != "", true)
}

func TestWtPendingFollowupStale(t *testing.T) {
	t.Parallel()
	h, _ := newTestHub(t)
	first, second := make(chan struct{}), make(chan struct{})
	releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
		if calls.Add(1) == 1 {
			close(first)
			<-releaseFirst
			return gitutil.GitStatusRefreshMsg{Status: " M old"}
		}
		close(second)
		<-releaseSecond
		return gitutil.GitStatusRefreshMsg{Status: " M new"}
	}
	e := h.register("t", t.TempDir(), gitutil.Options{})
	done := e.refresh("filesystem")
	<-first
	_ = e.refresh("filesystem")
	close(releaseFirst)
	<-second
	e.mu.Lock()
	stale := e.snapshotLocked().stale
	e.mu.Unlock()
	close(releaseSecond)
	<-done
	testutil.Equal(t, stale, true)
}
