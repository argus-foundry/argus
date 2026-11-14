package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/fsnotify/fsnotify"
)

type wtFakeWatcher struct {
	events       chan fsnotify.Event
	errs         chan error
	adds, closed atomic.Int32
	fail         bool
}

func (w *wtFakeWatcher) Add(string) error {
	w.adds.Add(1)
	if w.fail {
		return errors.New("cannot watch")
	}
	return nil
}
func (w *wtFakeWatcher) Remove(string) error              { return nil }
func (w *wtFakeWatcher) Close() error                     { w.closed.Add(1); return nil }
func (w *wtFakeWatcher) EventChan() <-chan fsnotify.Event { return w.events }
func (w *wtFakeWatcher) ErrorChan() <-chan error          { return w.errs }

func TestWtWatchBudget(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"entry", "process", "add failure", "new watcher failure"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := newTestHub(t)
			h.budget = &wtWatchBudget{limit: 100}
			h.entryBudget = 100
			w := &wtFakeWatcher{fail: mode == "add failure"}
			h.newWatcher = func() (wtWatcher, error) {
				if mode == "new watcher failure" {
					return nil, errors.New("open failed")
				}
				return w, nil
			}
			if mode == "entry" {
				h.entryBudget = 3
			}
			if mode == "process" {
				h.budget.limit = 3
			}
			e := h.register("t", wtRepo(t), gitutil.Options{})
			<-e.watchReady
			e.mu.Lock()
			degraded, watcher := e.degraded, e.watcher
			e.mu.Unlock()
			testutil.Equal(t, degraded, true)
			testutil.Nil(t, watcher)
			testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
			testutil.Equal(t, w.adds.Load() <= 2, true)
			if mode != "new watcher failure" {
				testutil.Equal(t, w.closed.Load(), int32(1))
			}
		})
	}
	t.Run("new subtrees share the entry allowance", func(t *testing.T) {
		h, _ := newTestHub(t)
		h.budget = &wtWatchBudget{limit: 100}
		w := &wtFakeWatcher{}
		h.newWatcher = func() (wtWatcher, error) { return w, nil }
		repo := wtRepo(t)
		e := h.register("t", repo, gitutil.Options{})
		<-e.watchReady
		e.watchMu.Lock()
		initial := e.watchUnits
		h.entryBudget = initial + 1
		e.watchMu.Unlock()
		for i, name := range []string{"new1", "new2"} {
			p := filepath.Join(e.watchRoot, name)
			testutil.NoError(t, os.Mkdir(p, 0o700))
			e.onFsEvent(w, fsnotify.Event{Name: p, Op: fsnotify.Create})
			if i == 0 {
				e.mu.Lock()
				degraded := e.degraded
				e.mu.Unlock()
				testutil.Equal(t, degraded, false)
				e.watchMu.Lock()
				units := e.watchUnits
				e.watchMu.Unlock()
				testutil.Equal(t, units, initial+1)
			}
		}
		e.mu.Lock()
		degraded := e.degraded
		e.mu.Unlock()
		testutil.Equal(t, degraded, true)
		testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
		testutil.Equal(t, int(w.adds.Load()) <= initial, true) // one unit belongs to the backend itself
		testutil.Equal(t, w.closed.Load(), int32(1))
	})
	t.Run("two entries share process capacity", func(t *testing.T) {
		h, _ := newTestHub(t)
		h.budget = &wtWatchBudget{limit: 100}
		h.newWatcher = func() (wtWatcher, error) { return &wtFakeWatcher{}, nil }
		repo := wtRepo(t)
		a := h.register("a", repo, gitutil.Options{})
		<-a.watchReady
		used := wtBudgetUsed(t, h.budget)
		h.budget.mu.Lock()
		h.budget.limit = used + 1
		h.budget.mu.Unlock()
		b := h.register("b", repo, gitutil.Options{})
		<-b.watchReady
		b.mu.Lock()
		degraded := b.degraded
		b.mu.Unlock()
		testutil.Equal(t, degraded, true)
		testutil.Equal(t, wtBudgetUsed(t, h.budget), used)
		h.remove("a")
		testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
	})
}

func TestWtColdSlowWalk(t *testing.T) {
	t.Parallel()
	h, _ := newTestHub(t)
	h.coldTimeout = 30 * time.Millisecond
	h.fetch = func(id, wt string, o gitutil.Options) gitutil.GitStatusRefreshMsg {
		return gitutil.GitStatusRefreshMsg{TaskID: id}
	}
	started, release := make(chan struct{}), make(chan struct{})
	real := h.walk
	var once sync.Once
	h.walk = func(root string, fn fs.WalkDirFunc) error {
		once.Do(func() { close(started) })
		<-release
		return real(root, fn)
	}
	repo := wtRepo(t)
	done := make(chan error, 1)
	go func() { _, err := h.status(context.Background(), "t", repo, gitutil.Options{}); done <- err }()
	<-started
	select {
	case err := <-done:
		testutil.NoError(t, err)
	case <-time.After(250 * time.Millisecond):
		t.Error("cold read blocked on watcher walk")
	}
	close(release)
	<-h.entries["t"].watchReady
}

func TestWtWatchFileBudget(t *testing.T) {
	t.Parallel()
	for _, files := range []bool{false, true} {
		t.Run(fmt.Sprint(files), func(t *testing.T) {
			h, _ := newTestHub(t)
			h.countFiles = files
			h.entryBudget = 32
			h.budget = &wtWatchBudget{limit: 64}
			h.newWatcher = func() (wtWatcher, error) { return &wtFakeWatcher{}, nil }
			repo := wtRepo(t)
			for i := range 100 {
				testutil.NoError(t, os.WriteFile(filepath.Join(repo, fmt.Sprint("file", i)), nil, 0o600))
			}
			e := h.register("t", repo, gitutil.Options{})
			<-e.watchReady
			e.mu.Lock()
			degraded := e.degraded
			e.mu.Unlock()
			testutil.Equal(t, degraded, files)
			testutil.Equal(t, wtBudgetUsed(t, h.budget) <= 32, true)
		})
	}
}

func TestWtDegradedStatus(t *testing.T) {
	srv, d := testServer(t)
	srv.wt.budget = &wtWatchBudget{limit: 32}
	srv.wt.entryBudget = 16
	srv.wt.countFiles = true
	repo := wtRepo(t)
	for i := range 100 {
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, fmt.Sprint("large", i)), nil, 0o600))
	}
	task := &model.Task{Name: "w", Status: model.StatusPending, Worktree: repo}
	testutil.NoError(t, d.Add(task))
	_, err := srv.wt.status(context.Background(), task.ID, repo, gitutil.Options{})
	testutil.NoError(t, err)
	<-srv.wt.entries[task.ID].watchReady
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/status", ""))
	testutil.Equal(t, w.Code, 200)
	var response gitStatusResponse
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	testutil.Equal(t, response.WatchDegraded, true)
	srv.wt.budget.mu.Lock()
	used := srv.wt.budget.used
	srv.wt.budget.mu.Unlock()
	testutil.Equal(t, used, 0)
}

func TestWtWatchSymlinkDirectory(t *testing.T) {
	t.Parallel()
	h, _ := newTestHub(t)
	h.countFiles = true
	h.budget = &wtWatchBudget{limit: 100}
	repo := wtRepo(t)
	testutil.NoError(t, os.Symlink(t.TempDir(), filepath.Join(repo, "linked")))
	e := h.register("t", repo, gitutil.Options{})
	<-e.watchReady
	e.mu.Lock()
	degraded := e.degraded
	e.mu.Unlock()
	testutil.Equal(t, degraded, true)
	h.budget.mu.Lock()
	used := h.budget.used
	h.budget.mu.Unlock()
	testutil.Equal(t, used, 0)
}

func TestWtWatchIgnoresRetiredEvents(t *testing.T) {
	t.Parallel()
	h, _ := newTestHub(t)
	h.budget = &wtWatchBudget{limit: 100}
	w := &wtFakeWatcher{}
	h.newWatcher = func() (wtWatcher, error) { return w, nil }
	repo := wtRepo(t)
	e := h.register("t", repo, gitutil.Options{})
	<-e.watchReady
	e.watchMu.Lock()
	e.degradeWatching(w, errors.New("retired"))
	e.watchMu.Unlock()
	adds := w.adds.Load()
	e.onFsEvent(w, fsnotify.Event{Name: repo, Op: fsnotify.Create})
	testutil.Equal(t, w.adds.Load(), adds)
	testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
}

func TestWtWatchRootRemoval(t *testing.T) {
	t.Parallel()
	h, _ := newTestHub(t)
	h.budget = &wtWatchBudget{limit: 100}
	w := &wtFakeWatcher{}
	h.newWatcher = func() (wtWatcher, error) { return w, nil }
	repo := wtRepo(t)
	e := h.register("t", repo, gitutil.Options{})
	<-e.watchReady
	testutil.NoError(t, os.RemoveAll(repo))
	e.onFsEvent(w, fsnotify.Event{Name: repo, Op: fsnotify.Remove})
	e.mu.Lock()
	degraded := e.degraded
	e.mu.Unlock()
	testutil.Equal(t, degraded, true)
	testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
}

func wtBudgetUsed(t *testing.T, b *wtWatchBudget) int {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
