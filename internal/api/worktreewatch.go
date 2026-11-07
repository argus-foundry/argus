package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/drn/argus/internal/gitutil"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/uxlog"
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
}

func newWtHub(emit func(model.Event)) *wtHub {
	return &wtHub{
		entries:     make(map[string]*wtEntry),
		emit:        emit,
		fetch:       gitutil.FetchGitStatus,
		debounce:    wtDebounce,
		coldTimeout: wtColdTimeout,
		maxAge:      wtMaxAge,
	}
}

type wtEntry struct {
	hub      *wtHub
	taskID   string
	worktree string
	base     string
	gitDir   string

	mutate sync.Mutex // serializes discards for this worktree

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
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

func statusRevision(msg gitutil.GitStatusRefreshMsg) string {
	b, err := json.Marshal(msg)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "r" + hex.EncodeToString(sum[:8])
}

// register returns the entry for the task, replacing it when the worktree or
// comparison base changed so a cached status is never served for another base.
func (h *wtHub) register(taskID, worktree string, opts gitutil.Options) *wtEntry {
	h.mu.Lock()
	if h.closed {
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
	h.entries[taskID] = e
	h.mu.Unlock()
	e.startWatching()
	return e
}

func newDetachedEntry(h *wtHub, taskID, worktree string, opts gitutil.Options) *wtEntry {
	return &wtEntry{hub: h, taskID: taskID, worktree: worktree, base: opts.BaseBranch, stop: make(chan struct{})}
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
}

// sweep drops entries whose task or worktree no longer exists.
func (h *wtHub) sweep(valid func(taskID, worktree string) bool) {
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
	w := e.watcher
	e.watcher = nil
	e.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

func (e *wtEntry) startWatching() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		uxlog.Log("[gitwatch] watcher unavailable task=%s: %v (falling back to max-age refresh)", e.taskID, err)
		return
	}
	if gd, gerr := gitutil.AbsoluteGitDir(e.worktree); gerr == nil {
		e.gitDir = gd
		if werr := w.Add(gd); werr != nil {
			uxlog.Log("[gitwatch] git dir watch failed task=%s: %v", e.taskID, werr)
		}
	}
	n := e.addTree(w, e.worktree)
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		_ = w.Close()
		return
	}
	e.watcher = w
	e.mu.Unlock()
	uxlog.Log("[gitwatch] registered task=%s dirs=%d", e.taskID, n)
	go e.loop(w)
}

func skipWatchDir(name string) bool {
	return name == ".git" || name == "node_modules"
}

func (e *wtEntry) addTree(w *fsnotify.Watcher, root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != root && skipWatchDir(d.Name()) {
			return filepath.SkipDir
		}
		if n >= wtMaxWatches {
			return filepath.SkipAll
		}
		if w.Add(p) == nil {
			n++
		}
		return nil
	})
	return n
}

func (e *wtEntry) loop(w *fsnotify.Watcher) {
	for {
		select {
		case <-e.stop:
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			e.onFsEvent(w, ev)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			uxlog.Log("[gitwatch] watcher error task=%s: %v", e.taskID, err)
			e.markDirty("filesystem")
		}
	}
}

func (e *wtEntry) onFsEvent(w *fsnotify.Watcher, ev fsnotify.Event) {
	if ev.Op == fsnotify.Chmod {
		return
	}
	if e.gitDir != "" && filepath.Dir(ev.Name) == e.gitDir {
		name := filepath.Base(ev.Name)
		if strings.HasSuffix(name, ".lock") || (name != "HEAD" && name != "index") {
			return
		}
		e.markDirty("git")
		return
	}
	if ev.Has(fsnotify.Create) {
		if fi, err := os.Lstat(ev.Name); err == nil && fi.IsDir() && !skipWatchDir(fi.Name()) {
			e.addTree(w, ev.Name)
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
		msg := e.hub.fetch(e.taskID, e.worktree, gitutil.Options{BaseBranch: e.base})

		e.mu.Lock()
		var emitRev string
		changed := false
		if msg.Err != nil {
			e.lastErr = msg.Err
			uxlog.Log("[gitwatch] status refresh failed task=%s: %v", e.taskID, msg.Err)
		} else {
			rev := statusRevision(msg)
			changed = e.msg != nil && rev != e.revision
			m := msg
			e.msg, e.revision, e.at, e.lastErr = &m, rev, time.Now(), nil
			e.dirty = false
			emitRev = rev
		}
		again := e.again
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
	return wtSnapshot{msg: *e.msg, revision: e.revision, stale: stale}
}

// status serves the cached snapshot, flagging it stale (and refreshing in the
// background) when a change is pending. A cold read waits for one bounded
// refresh.
func (h *wtHub) status(ctx context.Context, taskID, worktree string, opts gitutil.Options) (wtSnapshot, error) {
	e := h.register(taskID, worktree, opts)

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
	ch := e.refresh("discard")
	select {
	case <-ch:
	case <-time.After(h.coldTimeout):
	}
	e.mu.Lock()
	rev := ""
	if e.msg != nil && e.lastErr == nil {
		rev = e.revision
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
			s.wt.sweep(func(taskID, worktree string) bool {
				task, err := s.db.Get(taskID)
				return err == nil && task != nil && task.Worktree == worktree
			})
		}
	}
}
