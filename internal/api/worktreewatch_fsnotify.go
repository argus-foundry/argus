package api

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/fsnotify/fsnotify"
)

const wtWatchFiles = runtime.GOOS == "darwin"

func wtBackendUnits() int {
	if wtWatchFiles {
		return 3
	}
	return 1
}

type wtFSBackend interface {
	Add(string) error
	Remove(string) error
	Close() error
}

type wtFSWatcher struct {
	backend   wtFSBackend
	events    <-chan fsnotify.Event
	errors    <-chan error
	mu        sync.Mutex
	attempted map[string]bool
	closed    bool
	closeErr  error
}

func newWtWatcher() (wtWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &wtFSWatcher{backend: w, events: w.Events, errors: w.Errors, attempted: make(map[string]bool)}, nil
}

func (w *wtFSWatcher) Add(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fsnotify.ErrClosed
	}
	// Add may install a directory and some children before returning an error.
	w.attempted[path] = true
	return w.backend.Add(path)
}

func (w *wtFSWatcher) Remove(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fsnotify.ErrClosed
	}
	err := w.backend.Remove(path)
	if err == nil || errors.Is(err, fsnotify.ErrNonExistentWatch) {
		delete(w.attempted, path)
	}
	return err
}

func (w *wtFSWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.closeErr
	}
	paths := make([]string, 0, len(w.attempted))
	for path := range w.attempted {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	var errs []error
	// fsnotify 1.9.0 marks kqueue closed before Remove can close descriptors.
	for _, path := range paths {
		if err := w.backend.Remove(path); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
			errs = append(errs, fmt.Errorf("remove watch %s: %w", path, err))
		} else {
			delete(w.attempted, path)
		}
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	w.closed = true
	if err := w.backend.Close(); err != nil {
		// fsnotify may already be closed; a later nil cannot confirm cleanup.
		w.closeErr = err
		return err
	}
	for range w.events {
	}
	for range w.errors {
	}
	return nil
}

func (w *wtFSWatcher) EventChan() <-chan fsnotify.Event { return w.events }
func (w *wtFSWatcher) ErrorChan() <-chan error          { return w.errors }
