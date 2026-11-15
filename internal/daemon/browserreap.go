package daemon

import (
	"log/slog"
	"path/filepath"
	"time"

	"github.com/argus-foundry/argus/internal/browserproc"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// browserReaper is process-driven: a missing task row or exited session must
// not hide a detached browser. Failed reads reset grace and never signal.
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
	if err != nil {
		r.armed = map[string]int{}
		slog.Warn("browser reaper: inspection failed, skipping tick", "err", err)
		uxlog.Log("[browsercleanup] sweep inspection failed: %v", err)
		return
	}
	slog.Debug("browser reaper: scan complete", "processes", len(procs))
	uxlog.Log("[browsercleanup] sweep scan processes=%d", len(procs))
	byWT := map[string][]browserproc.Process{}
	for _, p := range procs {
		byWT[p.Worktree] = append(byWT[p.Worktree], p)
	}
	next := map[string]int{}
	for wt, ps := range byWT {
		if !r.eligible(wt, rows, live) {
			continue
		}
		next[wt] = r.armed[wt] + 1
		if next[wt] < 6 {
			continue
		}
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
		delete(next, wt)
	}
	r.armed = next
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
