package api

import (
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

func TestWtArchiveRegistrationOrder(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: wtRepo(t)}
	testutil.NoError(t, d.Add(task))
	read, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	eligible := srv.wt.eligible
	srv.wt.eligible = func(id string) bool {
		ok := eligible(id)
		if once.CompareAndSwap(false, true) {
			close(read)
			<-release
		}
		return ok
	}
	registered := make(chan *wtEntry, 1)
	go func() { registered <- srv.wt.register(task.ID, task.Worktree, gitutil.Options{}) }()
	<-read
	task.Archived = true
	testutil.NoError(t, d.Update(task))
	removed := make(chan struct{})
	go func() {
		srv.Emit(model.Event{Type: model.EventTypeTaskArchived, TaskID: task.ID, At: time.Now()})
		close(removed)
	}()
	close(release)
	e := <-registered
	<-removed
	<-e.watchReady
	testutil.Equal(t, srv.wt.count(), 0)
	late := srv.wt.register(task.ID, task.Worktree, gitutil.Options{})
	testutil.Equal(t, late.managed, false)
	testutil.Equal(t, srv.wt.count(), 0)
	late.close()
}

func TestWtDiscardArchiveFence(t *testing.T) {
	srv, d := testServer(t)
	repo := wtRepo(t)
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
	testutil.NoError(t, d.Add(task))
	path := filepath.Join(repo, "only-copy")
	testutil.NoError(t, os.WriteFile(path, []byte("keep"), 0o600))
	unlock := srv.taskLocks.Lock(task.ID)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- discardReq(srv, task.ID, `{"path":"only-copy","delete_untracked":true}`) }()
	var early *httptest.ResponseRecorder
	select {
	case early = <-done:
		t.Error("discard bypassed task lock")
	case <-time.After(30 * time.Millisecond):
	}
	task.Archived = true
	testutil.NoError(t, d.Update(task))
	unlock()
	if early != nil {
		assertDiscardBody(t, early, 409, "task_archived")
	} else {
		select {
		case w := <-done:
			assertDiscardBody(t, w, 409, "task_archived")
		case <-time.After(5 * time.Second):
			t.Fatal("discard stayed blocked after archive")
		}
	}
	b, err := os.ReadFile(path)
	testutil.NoError(t, err)
	testutil.Equal(t, string(b), "keep")
	assertDiscardBody(t, discardReq(srv, task.ID, `{"path":"only-copy","delete_untracked":true}`), 409, "task_archived")
}

func TestWtArchivedSweep(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: wtRepo(t)}
	testutil.NoError(t, d.Add(task))
	e := srv.wt.register(task.ID, task.Worktree, gitutil.Options{})
	<-e.watchReady
	task.Archived = true
	testutil.NoError(t, d.Update(task))
	srv.sweepWorktrees()
	testutil.Equal(t, srv.wt.count(), 0)
}
