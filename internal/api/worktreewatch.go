package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	wtDebounce    = 150 * time.Millisecond
	wtColdTimeout = 8 * time.Second
	wtMaxAge      = 15 * time.Second
	wtSweepEvery  = 30 * time.Second
	wtMaxWatches  = 4096
)

var errWtUnavailable = errors.New("git status unavailable")

// wtSnapshot is a point-in-time copy of a cached worktree status.
type wtSnapshot struct {
	msg      gitutil.GitStatusRefreshMsg
	revision string
	stale    bool
	degraded bool
}

// wtHub keeps one cached status + revision per task worktree, fed by
// filesystem watchers, and publishes worktree.changed when the revision moves.
// Watchers exist only for validated task worktrees plus their git directory.
type wtHub struct {
	mu      sync.Mutex
	entries map[string]*wtEntry
	closed  bool

	emit        func(model.Event)
	fetch       func(taskID, worktree string, opts gitutil.Options) gitutil.GitStatusRefreshMsg
	debounce    time.Duration
	coldTimeout time.Duration
	maxAge      time.Duration
	eligible    func(string) bool
	budget      *wtWatchBudget
	entryBudget int
	countFiles  bool
	walk        func(string, fs.WalkDirFunc) error
	newWatcher  func() (wtWatcher, error)
}

func newWtHub(emit func(model.Event)) *wtHub {
	return &wtHub{
		entries:     make(map[string]*wtEntry),
		emit:        emit,
		fetch:       gitutil.FetchGitStatus,
		debounce:    wtDebounce,
		coldTimeout: wtColdTimeout,
		maxAge:      wtMaxAge,
		budget:      wtProcessWatches,
		entryBudget: wtMaxWatches,
		countFiles:  wtWatchFiles,
		walk:        filepath.WalkDir,
		newWatcher:  newWtWatcher,
	}
}

type wtEntry struct {
	hub        *wtHub
	taskID     string
	worktree   string
	base       string
	gitDir     string
	watchRoot  string
	managed    bool
	watchMu    sync.Mutex
	watchPaths map[string]bool
	watchDirs  map[string]bool
	watchUnits int
	watchReady chan struct{}
	degraded   bool

	mutate sync.Mutex // serializes discards for this worktree

	mu       sync.Mutex
	watcher  wtWatcher
	msg      *gitutil.GitStatusRefreshMsg
	revision string
	at       time.Time
	dirty    bool
	lastErr  error
	inflight chan struct{}
	again    bool
	timer    *time.Timer
	reason   string
	stopped  bool
	stop     chan struct{}
}

func (h *wtHub) emitChanged(taskID, revision, reason string, paths []string) {
	payload := map[string]any{"reason": reason}
	if revision != "" {
		payload["revision"] = revision
	}
	if len(paths) > 0 {
		payload["paths"] = paths
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.emit(model.Event{Type: model.EventTypeWorktreeChanged, At: time.Now(), TaskID: taskID, Payload: b})
	uxlog.Log("[gitwatch] worktree.changed task=%s reason=%s revision=%q", taskID, reason, revision)
}

// register returns the entry for the task, replacing it when the worktree or
// comparison base changed so a cached status is never served for another base.
func (h *wtHub) register(taskID, worktree string, opts gitutil.Options) *wtEntry {
	h.mu.Lock()
	if h.closed || (h.eligible != nil && !h.eligible(taskID)) {
		if old := h.entries[taskID]; old != nil {
			delete(h.entries, taskID)
			go old.close()
		}
		h.mu.Unlock()
		return newDetachedEntry(h, taskID, worktree, opts)
	}
	if e, ok := h.entries[taskID]; ok {
		if e.worktree == worktree && e.base == opts.BaseBranch {
			h.mu.Unlock()
			return e
		}
		delete(h.entries, taskID)
		go e.close()
	}
	e := newDetachedEntry(h, taskID, worktree, opts)
	e.managed = true
	h.entries[taskID] = e
	h.mu.Unlock()
	go e.startWatching()
	return e
}

func newDetachedEntry(h *wtHub, taskID, worktree string, opts gitutil.Options) *wtEntry {
	return &wtEntry{hub: h, taskID: taskID, worktree: worktree, base: opts.BaseBranch, stop: make(chan struct{}), watchReady: make(chan struct{})}
}

// remove unregisters a task's watcher and cache (task deleted/archived).
func (h *wtHub) remove(taskID string) {
	h.mu.Lock()
	e := h.entries[taskID]
	delete(h.entries, taskID)
	h.mu.Unlock()
	if e != nil {
		e.close()
		uxlog.Log("[gitwatch] unregistered task=%s", taskID)
	}
}

func (h *wtHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

func (h *wtHub) closeAll() {
	h.mu.Lock()
	h.closed = true
	entries := h.entries
	h.entries = make(map[string]*wtEntry)
	h.mu.Unlock()
	for _, e := range entries {
		e.close()
	}
	h.budget.reconcile()
}

// sweep drops entries whose task or worktree no longer exists.
func (h *wtHub) sweep(valid func(taskID, worktree string) bool) {
	h.budget.reconcile()
	h.mu.Lock()
	var gone []string
	for id, e := range h.entries {
		if _, err := os.Stat(e.worktree); err != nil || !valid(id, e.worktree) {
			gone = append(gone, id)
		}
	}
	h.mu.Unlock()
	for _, id := range gone {
		h.remove(id)
	}
}

func (e *wtEntry) close() {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.stopped = true
	close(e.stop)
	if e.timer != nil {
		e.timer.Stop()
	}
	e.mu.Unlock()
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	e.mu.Lock()
	w := e.watcher
	e.watcher = nil
	e.mu.Unlock()
	e.releaseWatching(w)
}

func (e *wtEntry) startWatching() {
	defer close(e.watchReady)
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	select {
	case <-e.stop:
		return
	default:
	}
	root, err := filepath.EvalSymlinks(e.worktree)
	if err != nil {
		e.degradeWatching(nil, err)
		return
	}
	e.watchRoot = root
	gd, err := gitutil.AbsoluteGitDir(root)
	if err != nil {
		e.degradeWatching(nil, err)
		return
	}
	e.gitDir = gd
	if wtBackendUnits() > e.hub.entryBudget || !e.hub.budget.reserve(wtBackendUnits()) {
		e.degradeWatching(nil, errWtWatchBudget)
		return
	}
	e.watchUnits = wtBackendUnits()
	w, err := e.hub.newWatcher()
	if err != nil {
		e.degradeWatching(nil, err)
		return
	}
	e.watchPaths = make(map[string]bool)
	e.watchDirs = make(map[string]bool)
	if err = e.addGitWatches(w); err == nil {
		err = e.addTree(w, root)
	}
	if err != nil {
		e.degradeWatching(w, err)
		return
	}
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		e.degradeWatching(w, os.ErrClosed)
		return
	}
	e.watcher = w
	e.mu.Unlock()
	uxlog.Log("[gitwatch] registered task=%s units=%d", e.taskID, e.watchUnits)
	go e.loop(w)
	// Reconcile edits between the initial fetch and watcher installation.
	e.refresh("filesystem")
}

func skipWatchDir(name string) bool { return name == ".git" || name == "node_modules" }

func (e *wtEntry) addGitWatches(w wtWatcher) error {
	if err := e.reserveDirectory(e.gitDir); err != nil {
		return err
	}
	return e.addWatch(w, e.gitDir)
}

func (e *wtEntry) loop(w wtWatcher) {
	for {
		select {
		case <-e.stop:
			return
		case ev, ok := <-w.EventChan():
			if !ok {
				return
			}
			e.onFsEvent(w, ev)
		case err, ok := <-w.ErrorChan():
			if !ok {
				return
			}
			e.watchMu.Lock()
			e.degradeWatching(w, err)
			e.watchMu.Unlock()
			return
		}
	}
}

func (e *wtEntry) onFsEvent(w wtWatcher, ev fsnotify.Event) {
	if ev.Op == fsnotify.Chmod {
		return
	}
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	select {
	case <-e.stop:
		return
	default:
	}
	e.mu.Lock()
	active := e.watcher == w && !e.stopped
	e.mu.Unlock()
	if !active {
		return
	}
	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		if e.watchPaths[ev.Name] {
			err := w.Remove(ev.Name)
			if err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
				e.degradeWatching(w, err)
				return
			}
			delete(e.watchDirs, ev.Name)
			delete(e.watchPaths, ev.Name)
			e.watchUnits--
			e.hub.budget.release(1)
		}
	}

	isGit := ev.Name == e.gitDir || filepath.Dir(ev.Name) == e.gitDir
	if isGit {
		name := filepath.Base(ev.Name)
		if ev.Name != e.gitDir && (strings.HasSuffix(name, ".lock") || (name != "HEAD" && name != "index")) {
			return
		}
		if err := e.addGitWatches(w); err != nil {
			e.degradeWatching(w, err)
			return
		}
		e.markDirty("git")
		return
	}
	if ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove) {
		root := ev.Name
		if fi, err := os.Lstat(root); err != nil || !fi.IsDir() {
			root = filepath.Dir(root)
		}
		rel, err := filepath.Rel(e.watchRoot, root)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			e.degradeWatching(w, os.ErrNotExist)
			return
		}
		if !skipWatchDir(filepath.Base(root)) {
			if err := e.addTree(w, root); err != nil {
				e.degradeWatching(w, err)
				return
			}
		}
	}

	e.markDirty("filesystem")
}

// markDirty flags the cache stale and arms a debounced refresh; events inside
// the window are absorbed so bursts coalesce into one status computation.
func (e *wtEntry) markDirty(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return
	}
	e.dirty = true
	if e.reason == "" || reason == "git" {
		e.reason = reason
	}
	if e.timer == nil {
		e.timer = time.AfterFunc(e.hub.debounce, func() {
			e.mu.Lock()
			reason := e.reason
			e.reason = ""
			e.timer = nil
			e.mu.Unlock()
			e.refresh(reason)
		})
	}
}

// refresh runs (or joins) a single-flight status computation and returns a
// channel closed when a computation that started after this call completes.
func (e *wtEntry) refresh(reason string) <-chan struct{} {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	if e.inflight != nil {
		e.again = true
		if reason != "" {
			e.reason = reason
		}
		ch := e.inflight
		e.mu.Unlock()
		return ch
	}
	done := make(chan struct{})
	e.inflight = done
	e.mu.Unlock()
	go e.runRefresh(reason, done)
	return done
}

func (e *wtEntry) runRefresh(reason string, done chan struct{}) {
	for {
		e.reconcileWatching()
		msg := e.hub.fetch(e.taskID, e.worktree, gitutil.Options{BaseBranch: e.base})

		e.mu.Lock()
		var emitRev string
		changed := false
		if msg.Err != nil {
			e.lastErr = msg.Err
			uxlog.Log("[gitwatch] status refresh failed task=%s: %v", e.taskID, msg.Err)
		} else {
			rev := worktreeRevision(e.worktree, msg)
			changed = e.msg != nil && rev != e.revision
			m := msg
			e.msg, e.revision, e.at, e.lastErr = &m, rev, time.Now(), nil
			e.dirty = false
			emitRev = rev
		}
		again := e.again && !e.stopped
		if again {
			e.dirty = true
		}
		e.again = false
		if !again {
			e.inflight = nil
		}
		stopped := e.stopped
		e.mu.Unlock()

		if changed && !stopped && reason != "discard" {
			if reason == "" {
				reason = "filesystem"
			}
			e.hub.emitChanged(e.taskID, emitRev, reason, nil)
		}
		if !again {
			close(done)
			return
		}
		reason = ""
	}
}

func (e *wtEntry) snapshotLocked() wtSnapshot {
	stale := e.dirty || e.lastErr != nil || time.Since(e.at) > e.hub.maxAge
	return wtSnapshot{msg: *e.msg, revision: e.revision, stale: stale, degraded: e.degraded}
}

// status serves the cached snapshot, flagging it stale (and refreshing in the
// background) when a change is pending. A cold read waits for one bounded
// refresh.
func (h *wtHub) status(ctx context.Context, taskID, worktree string, opts gitutil.Options) (wtSnapshot, error) {
	e := h.register(taskID, worktree, opts)
	if !e.managed {
		defer e.close()
	}

	e.mu.Lock()
	if e.msg != nil {
		snap := e.snapshotLocked()
		e.mu.Unlock()
		if snap.stale {
			e.refresh("")
		}
		return snap, nil
	}
	e.mu.Unlock()

	ch := e.refresh("")
	t := time.NewTimer(h.coldTimeout)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
		return wtSnapshot{}, errWtUnavailable
	case <-ctx.Done():
		return wtSnapshot{}, ctx.Err()
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.msg != nil {
		return e.snapshotLocked(), nil
	}
	if e.lastErr != nil {
		return wtSnapshot{}, e.lastErr
	}
	return wtSnapshot{}, errWtUnavailable
}

// publishDiscard refreshes the cached status after a mutation has landed and
// then emits worktree.changed, so subscribers refetch an already-current view.
func (h *wtHub) publishDiscard(taskID, worktree string, opts gitutil.Options, paths []string) {
	e := h.register(taskID, worktree, opts)
	if !e.managed {
		defer e.close()
	}
	e.mu.Lock()
	e.dirty = true
	e.mu.Unlock()
	ch := e.refresh("discard")
	completed := false
	select {
	case <-ch:
		completed = true
	case <-time.After(h.coldTimeout):
	}
	e.mu.Lock()
	rev := ""
	if completed && e.msg != nil && e.lastErr == nil {
		rev = e.revision
	} else {
		e.dirty = true
	}
	e.mu.Unlock()
	h.emitChanged(taskID, rev, "discard", paths)
}

func (s *Server) worktreeSweeper() {
	t := time.NewTicker(wtSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.sweepWorktrees()
		}
	}
}

func (s *Server) sweepWorktrees() {
	s.wt.sweep(func(taskID, worktree string) bool {
		task, err := s.db.Get(taskID)
		return err == nil && task != nil && !task.Archived && task.Worktree == worktree
	})
}
