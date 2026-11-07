package api

import (
	"errors"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/fsnotify/fsnotify"
)

type wtTransientClose struct {
	wtFakeWatcher
	calls int
}

func (w *wtTransientClose) Close() error {
	w.calls++
	if w.calls == 1 {
		return errors.New("transient cleanup failure")
	}
	return nil
}

func TestWtCleanupRetry(t *testing.T) {
	for _, mode := range []string{"degrade refresh", "remove allocation", "sweep", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := newTestHub(t)
			h.budget = &wtWatchBudget{limit: 10}
			w := &wtTransientClose{}
			e := newDetachedEntry(h, "retired", t.TempDir(), gitutil.Options{})
			testutil.Equal(t, h.budget.reserve(10), true)
			e.watchUnits, e.watcher = 10, w
			h.entries[e.taskID] = e
			if mode == "degrade refresh" {
				e.watchMu.Lock()
				e.degradeWatching(w, errors.New("degrade"))
				e.watchMu.Unlock()
			} else {
				h.remove(e.taskID)
			}
			testutil.Equal(t, wtBudgetUsed(t, h.budget), 10)
			testutil.Equal(t, w.calls, 1)
			switch mode {
			case "degrade refresh":
				e.reconcileWatching()
			case "remove allocation":
				testutil.Equal(t, h.budget.reserve(10), true)
				h.budget.release(10)
			case "sweep":
				h.sweep(func(string, string) bool { return true })
			case "shutdown":
				h.closeAll()
			}
			testutil.Equal(t, w.calls, 2)
			testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
			testutil.Equal(t, h.budget.reserve(10), true)
			h.budget.release(10)
			h.closeAll()
			testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
			testutil.Equal(t, w.calls, 2)
		})
	}
}

type wtRetryRemoval struct {
	wtFakeWatcher
	removes  int
	closeErr error
}

func (w *wtRetryRemoval) Remove(string) error {
	w.removes++
	if w.removes == 1 {
		return errors.New("transient removal failure")
	}
	return nil
}
func (w *wtRetryRemoval) Close() error {
	w.closed.Add(1)
	return w.closeErr
}

func wtTestFSWatcher(backend wtFSBackend) *wtFSWatcher {
	events, errs := make(chan fsnotify.Event), make(chan error)
	close(events)
	close(errs)
	return &wtFSWatcher{backend: backend, events: events, errors: errs, attempted: make(map[string]bool)}
}

func TestWtFSRemovalRetry(t *testing.T) {
	backend := &wtRetryRemoval{}
	w := wtTestFSWatcher(backend)
	testutil.NoError(t, w.Add("root"))
	b := &wtWatchBudget{limit: 10}
	testutil.Equal(t, b.reserve(10), true)
	e := newDetachedEntry(&wtHub{budget: b}, "retired", t.TempDir(), gitutil.Options{})
	e.watchUnits = 10
	e.releaseWatching(w)
	testutil.Equal(t, wtBudgetUsed(t, b), 10)
	testutil.Equal(t, backend.closed.Load(), int32(0))
	testutil.Equal(t, len(w.attempted), 1)
	// A new entry can reuse the retired entry's capacity after removal recovers.
	testutil.Equal(t, b.reserve(10), true)
	testutil.Equal(t, backend.removes, 2)
	testutil.Equal(t, backend.closed.Load(), int32(1))
	testutil.Equal(t, len(w.attempted), 0)
	testutil.Equal(t, len(b.pending), 0)
	b.release(10)
	b.reconcile()
	testutil.Equal(t, wtBudgetUsed(t, b), 0)
	testutil.Equal(t, backend.closed.Load(), int32(1))
}

func TestWtFSBackendCloseFailure(t *testing.T) {
	backend := &wtRetryRemoval{removes: 1, closeErr: errors.New("uncertain backend close")}
	w := wtTestFSWatcher(backend)
	testutil.NoError(t, w.Add("root"))
	b := &wtWatchBudget{limit: 10}
	testutil.Equal(t, b.reserve(10), true)
	e := newDetachedEntry(&wtHub{budget: b}, "retired", t.TempDir(), gitutil.Options{})
	e.watchUnits = 10
	e.releaseWatching(w)
	backend.closeErr = nil
	testutil.Equal(t, b.reserve(1), false)
	testutil.Equal(t, wtBudgetUsed(t, b), 10)
	testutil.Equal(t, backend.closed.Load(), int32(1))
	testutil.Equal(t, len(b.pending), 1)
}

func TestWtCleanupConcurrentReconcile(t *testing.T) {
	b := &wtWatchBudget{limit: 10}
	w := &wtTransientClose{}
	testutil.Equal(t, b.reserve(10), true)
	e := newDetachedEntry(&wtHub{budget: b}, "retired", t.TempDir(), gitutil.Options{})
	e.watchUnits = 10
	e.releaseWatching(w)
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(b.reconcile)
	}
	workers.Wait()
	testutil.Equal(t, w.calls, 2)
	testutil.Equal(t, wtBudgetUsed(t, b), 0)
	testutil.Equal(t, len(b.pending), 0)
}
