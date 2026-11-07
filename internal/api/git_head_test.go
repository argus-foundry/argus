package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drn/argus/internal/gitutil"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type headBody struct {
	Head *struct {
		Branch   *string `json:"branch"`
		OID      *string `json:"oid"`
		Detached bool    `json:"detached"`
	} `json:"head"`
	GitRevision string `json:"git_revision"`
}

func getHead(t *testing.T, srv *Server, id string) (int, headBody) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+id+"/git/status", ""))
	var b headBody
	if w.Code == http.StatusOK {
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &b))
	}
	return w.Code, b
}

func TestGitStatusHead(t *testing.T) {
	setup := func(t *testing.T) (*Server, *model.Task, string, chan model.Event) {
		srv, d := testServer(t)
		srv.wt.debounce = 30 * time.Millisecond
		repo := wtRepo(t)
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo, Branch: "argus/recorded"}
		testutil.NoError(t, d.Add(task))
		ch, cancel := srv.eventBus.subscribe()
		t.Cleanup(cancel)
		return srv, task, repo, ch
	}
	waitChanged := func(t *testing.T, ch chan model.Event) {
		t.Helper()
		for {
			select {
			case ev := <-ch:
				if ev.Type == model.EventTypeWorktreeChanged {
					return
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no worktree.changed")
			}
		}
	}

	t.Run("checkout updates head and revision; Task.Branch stays recorded", func(t *testing.T) {
		srv, task, repo, ch := setup(t)
		code, before := getHead(t, srv, task.ID)
		testutil.Equal(t, code, http.StatusOK)
		testutil.Equal(t, *before.Head.Branch, "master")
		testutil.Equal(t, before.Head.Detached, false)
		testutil.Equal(t, before.Head.OID != nil, true)

		runGit(t, repo, "checkout", "-q", "-b", "feature-name")
		waitChanged(t, ch)
		_, after := getHead(t, srv, task.ID)
		testutil.Equal(t, *after.Head.Branch, "feature-name")
		testutil.Equal(t, after.GitRevision != before.GitRevision, true)

		got, err := srv.db.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Branch, "argus/recorded")
	})

	t.Run("branch rename", func(t *testing.T) {
		srv, task, repo, ch := setup(t)
		getHead(t, srv, task.ID)
		runGit(t, repo, "branch", "-m", "renamed")
		waitChanged(t, ch)
		_, b := getHead(t, srv, task.ID)
		testutil.Equal(t, *b.Head.Branch, "renamed")
	})

	t.Run("detached head has null branch", func(t *testing.T) {
		srv, task, repo, ch := setup(t)
		getHead(t, srv, task.ID)
		runGit(t, repo, "checkout", "-q", "--detach")
		waitChanged(t, ch)
		_, b := getHead(t, srv, task.ID)
		testutil.Nil(t, b.Head.Branch)
		testutil.Equal(t, b.Head.Detached, true)
		testutil.Equal(t, b.Head.OID != nil, true)
	})

	t.Run("unborn branch has null oid", func(t *testing.T) {
		srv, d := testServer(t)
		repo := t.TempDir()
		runGit(t, repo, "init", "-q", "-b", "fresh")
		task := &model.Task{Name: "u", Status: model.StatusPending, Worktree: repo}
		testutil.NoError(t, d.Add(task))
		code, b := getHead(t, srv, task.ID)
		testutil.Equal(t, code, http.StatusOK)
		testutil.Equal(t, *b.Head.Branch, "fresh")
		testutil.Nil(t, b.Head.OID)
	})

	t.Run("unreadable head is 500 git_head_unavailable, never stored state", func(t *testing.T) {
		srv, d := testServer(t)
		srv.wt.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
			return gitutil.GitStatusRefreshMsg{TaskID: id, HeadErr: errors.New("boom")}
		}
		task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: t.TempDir(), Branch: "argus/recorded"}
		testutil.NoError(t, d.Add(task))
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		var body map[string]string
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		testutil.Equal(t, body["code"], "git_head_unavailable")
	})
}
