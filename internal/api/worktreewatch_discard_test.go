package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestWtDiscardRefreshFailure(t *testing.T) {
	h, rec := newTestHub(t)
	h.newWatcher = func() (wtWatcher, error) { return nil, errors.New("unwatched test") }
	h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
		return gitutil.GitStatusRefreshMsg{Status: " M before"}
	}
	repo := wtRepo(t)
	_, err := h.status(context.Background(), "t", repo, gitutil.Options{})
	testutil.NoError(t, err)
	<-h.entries["t"].watchReady
	h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
		return gitutil.GitStatusRefreshMsg{Err: errors.New("git failed")}
	}
	h.publishDiscard("t", repo, gitutil.Options{}, []string{"f.txt"})
	_, p := rec.next(t)
	testutil.Equal(t, p["revision"], nil)
	e := h.entries["t"]
	e.mu.Lock()
	stale := e.snapshotLocked().stale
	e.mu.Unlock()
	testutil.Equal(t, stale, true)
}

func TestHandleDiscardErrorBodies(t *testing.T) {
	for _, kind := range []string{"conflict", "git failure", "untracked", "unknown task", "missing worktree"} {
		t.Run(kind, func(t *testing.T) {
			srv, d := testServer(t)
			repo := wtRepo(t)
			task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
			testutil.NoError(t, d.Add(task))
			code, status, id := "", 0, task.ID
			path := "f.txt"
			switch kind {
			case "conflict":
				runGit(t, repo, "checkout", "-q", "-b", "other")
				testutil.NoError(t, os.WriteFile(filepath.Join(repo, path), []byte("other\n"), 0o600))
				runGit(t, repo, "commit", "-qam", "other")
				runGit(t, repo, "checkout", "-q", "master")
				testutil.NoError(t, os.WriteFile(filepath.Join(repo, path), []byte("main\n"), 0o600))
				runGit(t, repo, "commit", "-qam", "main")
				cmd := exec.Command("git", "merge", "other")
				cmd.Dir = repo
				testutil.Equal(t, cmd.Run() != nil, true)
				code, status = "conflict_state", 409
			case "git failure":
				testutil.NoError(t, os.RemoveAll(filepath.Join(repo, ".git")))
				code, status = "git_failed", 500
			case "untracked":
				path = "new.txt"
				testutil.NoError(t, os.WriteFile(filepath.Join(repo, path), []byte("keep"), 0o600))
				code, status = "untracked_requires_delete", 409
			case "unknown task":
				code, status, id = "task_not_found", 404, "missing"
			case "missing worktree":
				testutil.NoError(t, os.RemoveAll(repo))
				code, status = "worktree_not_found", 404
			}
			before, _ := os.ReadFile(filepath.Join(repo, path))
			index, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
			ch, cancel := srv.eventBus.subscribe()
			defer cancel()
			w := discardReq(srv, id, `{"path":"`+path+`"}`)
			assertDiscardBody(t, w, status, code)
			after, _ := os.ReadFile(filepath.Join(repo, path))
			newIndex, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
			testutil.Equal(t, string(after), string(before))
			testutil.Equal(t, string(newIndex), string(index))
			for {
				select {
				case ev := <-ch:
					testutil.Equal(t, strings.Contains(string(ev.Payload), `"reason":"discard"`), false)
				default:
					return
				}
			}
		})
	}
}

func TestWtWarmFailureHTTP(t *testing.T) {
	srv, d := testServer(t)
	srv.wt.newWatcher = func() (wtWatcher, error) { return nil, errors.New("no watcher") }
	var fail atomic.Bool
	srv.wt.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
		if fail.Load() {
			return gitutil.GitStatusRefreshMsg{Err: errors.New("git failed")}
		}
		return gitutil.GitStatusRefreshMsg{TaskID: id, Status: " M cached"}
	}
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: wtRepo(t)}
	testutil.NoError(t, d.Add(task))
	_, err := srv.wt.status(context.Background(), task.ID, task.Worktree, gitutil.Options{})
	testutil.NoError(t, err)
	<-srv.wt.entries[task.ID].watchReady
	fail.Store(true)
	<-srv.wt.entries[task.ID].refresh("")
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
	testutil.Equal(t, w.Code, 200)
	var response gitStatusResponse
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	testutil.Equal(t, response.Stale, true)
	testutil.Equal(t, response.Status, " M cached")
}
