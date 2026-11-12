package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/fsnotify/fsnotify"
)

type wtQuietWatcher struct{ wtWatcher }

func (w *wtQuietWatcher) EventChan() <-chan fsnotify.Event { return nil }
func (w *wtQuietWatcher) ErrorChan() <-chan error          { return nil }

func TestWtRefreshRecountsWatches(t *testing.T) {
	if !wtWatchFiles {
		t.Skip("Darwin implicit child accounting")
	}
	h, _ := newTestHub(t)
	h.budget = &wtWatchBudget{limit: 256}
	h.entryBudget = 128
	h.newWatcher = func() (wtWatcher, error) {
		w, err := newWtWatcher()
		if err != nil {
			return nil, err
		}
		return &wtQuietWatcher{w}, nil
	}
	repo := wtRepo(t)
	_, err := h.status(context.Background(), "t", repo, gitutil.Options{})
	testutil.NoError(t, err)
	e := h.entries["t"]
	<-e.watchReady
	<-e.refresh("")
	for i := range 200 {
		testutil.NoError(t, os.WriteFile(filepath.Join(repo, fmt.Sprint("drift", i)), nil, 0o600))
	}
	h.maxAge = 0
	_, err = h.status(context.Background(), "t", repo, gitutil.Options{})
	testutil.NoError(t, err)
	<-e.refresh("")
	e.mu.Lock()
	degraded := e.degraded
	e.mu.Unlock()
	testutil.Equal(t, degraded, true)
	h.budget.mu.Lock()
	used := h.budget.used
	h.budget.mu.Unlock()
	testutil.Equal(t, used, 0)
}

type wtCloseFailure struct{ wtWatcher }

func (w *wtCloseFailure) Close() error {
	_ = w.wtWatcher.Close()
	return errors.New("remove could not be confirmed")
}

func TestWtFailedTeardownKeepsReservations(t *testing.T) {
	for _, mode := range []string{"remove", "degrade"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := newTestHub(t)
			h.budget = &wtWatchBudget{limit: 100}
			h.newWatcher = func() (wtWatcher, error) { return &wtCloseFailure{&wtFakeWatcher{}}, nil }
			e := h.register("t", wtRepo(t), gitutil.Options{})
			<-e.watchReady
			e.watchMu.Lock()
			units := e.watchUnits
			e.watchMu.Unlock()
			if mode == "remove" {
				h.remove("t")
			} else {
				e.watchMu.Lock()
				e.degradeWatching(e.watcher, errors.New("degrade"))
				e.watchMu.Unlock()
			}
			h.budget.mu.Lock()
			used := h.budget.used
			h.budget.mu.Unlock()
			testutil.Equal(t, used, units)
			testutil.Equal(t, used > 0, true)
		})
	}
}
