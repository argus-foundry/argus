package api

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/argus-foundry/argus/internal/uxlog"
	"github.com/fsnotify/fsnotify"
)

var errWtWatchBudget = errors.New("worktree watch budget exhausted")
var wtProcessWatches = &wtWatchBudget{limit: wtProcessWatchLimit()}

type wtWatchBudget struct {
	mu          sync.Mutex
	limit, used int
	cleanupMu   sync.Mutex
	pending     []wtWatchCleanup
}

func (b *wtWatchBudget) reserve(n int) bool {
	b.reconcile()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+n > b.limit {
		return false
	}
	b.used += n
	return true
}
func (b *wtWatchBudget) release(n int) { b.mu.Lock(); b.used -= n; b.mu.Unlock() }

type wtWatcher interface {
	Add(string) error
	Remove(string) error
	Close() error
	EventChan() <-chan fsnotify.Event
	ErrorChan() <-chan error
}

// Reservations remain allocated until the serialized watcher close completes.
func (e *wtEntry) reservePath(path string) error {
	if e.watchPaths[path] {
		return nil
	}
	if e.watchUnits >= e.hub.entryBudget || !e.hub.budget.reserve(1) {
		return errWtWatchBudget
	}
	e.watchUnits++
	e.watchPaths[path] = true
	return nil
}

func (e *wtEntry) reserveDirectory(path string) error {
	if err := e.reservePath(path); err != nil {
		return err
	}
	if !e.hub.countFiles {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(64)
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				if target, err := os.Stat(child); err == nil && target.IsDir() {
					return errors.New("symlink directory requires polling")
				}
			}
			if err := e.reservePath(child); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (e *wtEntry) addWatch(w wtWatcher, path string) error {
	if e.watchDirs[path] {
		return e.reserveDirectory(path)
	}
	if err := w.Add(path); err != nil {
		return err
	}
	e.watchDirs[path] = true
	return e.reserveDirectory(path)
}

func (e *wtEntry) addTree(w wtWatcher, root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), e.hub.coldTimeout)
	defer cancel()
	var dirs []string
	err := e.hub.walk(root, func(p string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.stop:
			return fs.SkipAll
		default:
		}
		if err != nil {
			return err
		}
		if e.hub.countFiles {
			if err := e.reservePath(p); err != nil {
				return err
			}
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && skipWatchDir(d.Name()) {
			return filepath.SkipDir
		}
		if err := e.reserveDirectory(p); err != nil {
			return err
		}
		dirs = append(dirs, p)
		return nil
	})
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		select {
		case <-e.stop:
			return fs.SkipAll
		default:
		}
		if err := e.addWatch(w, dir); err != nil {
			return err
		}
	}
	return nil
}

func (e *wtEntry) degradeWatching(w wtWatcher, err error) {
	e.releaseWatching(w)
	e.watchPaths = nil
	e.mu.Lock()
	e.watcher = nil
	e.degraded = true
	e.mu.Unlock()
	uxlog.Log("[gitwatch] watcher degraded task=%s: %v (max-age refresh only)", e.taskID, err)
}

func (e *wtEntry) releaseWatching(w wtWatcher) {
	units := e.watchUnits
	e.watchUnits = 0
	if w != nil {
		if err := w.Close(); err != nil {
			e.hub.budget.retain(w, units, e.taskID)
			uxlog.Log("[gitwatch] teardown failed task=%s retaining=%d: %v", e.taskID, units, err)
			return
		}
	}
	e.hub.budget.release(units)
}

func (e *wtEntry) reconcileWatching() {
	e.hub.budget.reconcile()
	e.mu.Lock()
	active := e.watcher != nil && !e.stopped
	e.mu.Unlock()
	if !active {
		return
	}
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	e.mu.Lock()
	w, stopped := e.watcher, e.stopped
	e.mu.Unlock()
	if w == nil || stopped {
		return
	}
	for path := range e.watchDirs {
		if err := e.reserveDirectory(path); err != nil {
			e.degradeWatching(w, err)
			return
		}
	}
}
