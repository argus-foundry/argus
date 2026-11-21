package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/argus-foundry/argus/internal/browserproc"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// browserReaper is process-driven: a missing task row or exited session must
// not hide a detached browser. Whole-scan failures reset all grace; candidate
// inspection failures reset only that captured process's evidence.
type browserReaper struct {
	running func() ([]string, error)
	tasks   devStackReapTasks
	root    string
	scan    func(string) ([]browserproc.Process, error)
	stop    func([]browserproc.Process, browserproc.Options)
	armed   map[string]int
	cancel  <-chan struct{}
}

func newBrowserReaper(running func() ([]string, error), tasks devStackReapTasks, root string) *browserReaper {
	return &browserReaper{running: running, tasks: tasks, root: root, scan: browserproc.Scan, stop: browserproc.Stop, armed: map[string]int{}}
}

func (r *browserReaper) gather() ([]*model.Task, map[string]bool, error) {
	rows, err := r.tasks.TasksChecked()
	if err != nil {
		return nil, nil, err
	}
	ids, err := r.running()
	if err != nil {
		return nil, nil, err
	}
	live := map[string]bool{}
	for _, id := range ids {
		live[id] = true
	}
	return rows, live, nil
}

func (r *browserReaper) eligible(wt string, rows []*model.Task, live map[string]bool) bool {
	if browserproc.Worktree(wt, r.root) != wt {
		return false
	}
	known := map[string]bool{}
	for _, t := range rows {
		known[t.ID] = true
		if browserproc.Worktree(t.Worktree, r.root) == wt && (t.IsResident() || !taskFinished(t) || live[t.ID]) {
			return false
		}
	}
	// A live session whose row cannot be found has unknown cwd. It cannot
	// safely be interpreted as a deleted, inactive task.
	for id := range live {
		if !known[id] {
			return false
		}
	}
	return true
}

func (r *browserReaper) tick() {
	rows, live, err := r.gather()
	var procs []browserproc.Process
	if err == nil {
		procs, err = r.scan(r.root)
	}
	var partial *browserproc.PartialInspectionError
	if err != nil && !errors.As(err, &partial) {
		r.armed = map[string]int{}
		slog.Warn("browser reaper: inspection failed, skipping tick", "err", err)
		uxlog.Log("[browsercleanup] sweep inspection failed: %v", err)
		return
	}
	if partial != nil {
		slog.Warn("browser reaper: partial inspection", "skipped", len(partial.PIDs))
		uxlog.Log("[browsercleanup] sweep partial inspection skipped=%d", len(partial.PIDs))
	}
	slog.Debug("browser reaper: scan complete", "processes", len(procs))
	uxlog.Log("[browsercleanup] sweep scan processes=%d", len(procs))
	next := map[string]int{}
	byWT := map[string][]browserproc.Process{}
	for _, p := range procs {
		if !r.eligible(p.Worktree, rows, live) {
			continue
		}
		key := browserGraceKey(p)
		next[key] = r.armed[key] + 1
		if next[key] >= 6 {
			byWT[p.Worktree] = append(byWT[p.Worktree], p)
		}
	}
	for wt, ps := range byWT {
		select {
		case <-r.cancel:
			return
		default:
		}
		proceed := func() bool { fresh, running, e := r.gather(); return e == nil && r.eligible(wt, fresh, running) }
		if !proceed() {
			continue
		}
		slog.Info("browser reaper: stopping finished or missing task browsers", "worktree", wt, "processes", len(ps))
		uxlog.Log("[browsercleanup] sweep worktree=%s processes=%d", wt, len(ps))
		r.stop(ps, browserproc.Options{Cancel: r.cancel, Proceed: proceed})
		for _, p := range ps {
			delete(next, browserGraceKey(p))
		}
	}
	r.armed = next
}

func browserGraceKey(p browserproc.Process) string {
	return fmt.Sprintf("%d:%d:%t:%q:%q:%q", p.PID, p.PGID, p.Root, p.Command, p.CWD, p.Worktree)
}

func (d *Daemon) runBrowserReaper() {
	r := newBrowserReaper(runningFn(d.runner), d.db, filepath.Join(db.DataDir(), "worktrees"))
	r.cancel = d.done
	r.run(10 * time.Second)
}

func (r *browserReaper) run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.cancel:
			return
		case <-ticker.C:
			r.tick()
		}
	}
}
