package hera

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// An unreadable live-binding list fails closed: before the fix it read as
// "bound elsewhere", skipped the stop and recheck, and still nuked the role
// (ending the only binding user_deleted for the async sweep to reclaim).
func TestNukeResidentWorkerChecked_BindingsUnreadableRefuses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		recheck func(*model.Task) error
	}{
		{"non-force", func(*model.Task) error { return nil }},
		{"force", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
			d, _, view := residentWorkerNukeFixture(t, task)
			var events []string
			runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}
			rename := func(from, to string) {
				testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error {
					_, e := tx.Exec(`ALTER TABLE ` + from + ` RENAME TO ` + to)
					return e
				}))
			}
			rename("hera_bindings", "hera_bindings_hidden")
			_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: tc.recheck, Wait: fastStopWait})
			rename("hera_bindings_hidden", "hera_bindings")
			testutil.Error(t, err)
			testutil.Contains(t, err.Error(), "nothing changed")
			testutil.Equal(t, len(events), 0)
			r, rErr := d.HeraRole(view.RoleID)
			testutil.NoError(t, rErr)
			testutil.True(t, r.NukedAt == nil)
			b, bErr := d.HeraLiveBindingByTask(task.ID)
			testutil.NoError(t, bErr)
			testutil.Equal(t, b.RoleID, view.RoleID)
			got, gErr := d.Get(task.ID)
			testutil.NoError(t, gErr)
			testutil.False(t, got.Archived)
			testutil.Equal(t, got.Status, model.StatusInProgress)
		})
	}
}

// Force delete with a presence probe that always fails: the reclaim still
// stops the session (unreadable = present) and defers the prune to the sweep,
// logging it where the daemon can see it (slog).
func TestNukeResidentWorkerChecked_ForcePresenceUnreadableDefersPrune(t *testing.T) {
	logs := captureResidentSlog(t)
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	var events []string
	runner := &probingNukeRunner{
		recordingNukeRunner: &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events},
		probeErr:            func(bool) error { return errProbe },
	}
	_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{})
	testutil.NoError(t, err)
	testutil.DeepEqual(t, events, []string{"stop"})
	r, rErr := d.HeraRole(view.RoleID)
	testutil.NoError(t, rErr)
	testutil.True(t, r.NukedAt != nil)
	got, gErr := d.Get(task.ID)
	testutil.NoError(t, gErr) // not pruned
	testutil.True(t, got.Archived)
	testutil.Contains(t, logs.String(), "prune deferred to reclaim sweep task=w")
	testutil.Contains(t, logs.String(), "session presence unreadable task=w")
}

// A session that exits on Stop but has not settled by the end of the wait is
// refused ("had not settled"), the recheck never runs, and the worker is left
// resumable. Covers both the wait measured from the Stop call's START (not
// its return) and GiveUpBy capping it.
func TestNukeResidentWorkerChecked_ExitedNotSettledRefuses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stopTakes time.Duration
		giveUp    time.Duration // after the start; 0 = none
	}{
		// Stop takes 5s; Max (10s from the Stop's start) ends 5s after it
		// returned, before the 6s settle.
		{"slow stop", 5 * time.Second, 0},
		// Instant stop; GiveUpBy ends the wait at 3s, before the 6s settle.
		{"give up by", 0, 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
			d, _, view := residentWorkerNukeFixture(t, task)
			var events []string
			clk := newFakeStopClock()
			runner := &slowStopRunner{recordingNukeRunner: &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}, clk: clk, stopTakes: tc.stopTakes}
			opts := ResidentDeleteOptions{Clock: clk.clock(),
				Recheck: func(*model.Task) error {
					events = append(events, "recheck")
					return nil
				},
				Wait: StopWait{Max: 10 * time.Second, Settle: 6 * time.Second, Poll: 100 * time.Millisecond}}
			if tc.giveUp > 0 {
				opts.GiveUpBy = clk.Now().Add(tc.giveUp)
			}
			_, err := NukeResidentWorkerChecked(d, runner, view, opts)
			testutil.ErrorIs(t, err, ErrWorkerSessionStillRunning)
			testutil.Contains(t, err.Error(), "had not settled")
			testutil.DeepEqual(t, events, []string{"stop"})
			r, rErr := d.HeraRole(view.RoleID)
			testutil.NoError(t, rErr)
			testutil.True(t, r.NukedAt == nil)
			got, gErr := d.Get(task.ID)
			testutil.NoError(t, gErr)
			testutil.Equal(t, got.Status, model.StatusInReview)
			awaiting, aErr := d.HeraWorkerAwaitingCloseout(task.ID)
			testutil.NoError(t, aErr)
			testutil.False(t, awaiting)
		})
	}
}

// repoLockFix is a resident worker whose worktree directory exists in a
// project with a repo path, so the delete reclaims under the repo lock.
type repoLockFix struct {
	d      *db.DB
	task   *model.Task
	view   *heramodel.RoleView
	repo   string
	runner *recordingNukeRunner
	events *[]string
	opts   ResidentDeleteOptions
	clk    *fakeStopClock
}

func newRepoLockFix(t *testing.T) *repoLockFix {
	t.Helper()
	repo := t.TempDir()
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.NoError(t, os.MkdirAll(wt, 0o755))
	task := &model.Task{ID: "w", Name: "w", Project: "p", Worktree: wt, Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: repo}))
	events := []string{}
	f := &repoLockFix{d: d, task: task, view: view, repo: repo, events: &events, clk: newFakeStopClock()}
	f.runner = &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: f.events}
	f.opts = ResidentDeleteOptions{Clock: f.clk.clock(), Wait: StopWait{Max: 10 * time.Second, Settle: time.Second, Poll: 100 * time.Millisecond}}
	return f
}

func (f *repoLockFix) nuked(t *testing.T) bool {
	t.Helper()
	r, err := f.d.HeraRole(f.view.RoleID)
	testutil.NoError(t, err)
	return r.NukedAt != nil
}

// Another cleanup holding the repo's lock past MutateBy refuses "host busy"
// before the first change; the resident delete never queues on it after the
// stop.
func TestNukeResidentWorkerChecked_RepoLockBusyBeforeStop(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-force", true: "force"}[force], func(t *testing.T) {
			logs := captureResidentSlog(t)
			f := newRepoLockFix(t)
			unlock, err := agent.LockRepoContext(context.Background(), f.repo)
			testutil.NoError(t, err)
			released := make(chan struct{})
			done := make(chan struct{})
			// Old code would block on the lock after the nuke; release it
			// eventually so a regression fails instead of hanging.
			go func() {
				defer close(done)
				select {
				case <-released:
				case <-time.After(5 * time.Second):
				}
				unlock()
			}()
			if !force {
				f.opts.Recheck = func(*model.Task) error { return nil }
			}
			f.opts.MutateBy = f.clk.Now().Add(2 * time.Second)
			_, err = NukeResidentWorkerChecked(f.d, f.runner, f.view, f.opts)
			close(released)
			<-done
			testutil.ErrorIs(t, err, ErrHostBusy)
			testutil.Contains(t, err.Error(), "held its lock")
			testutil.Equal(t, len(*f.events), 0)
			testutil.False(t, f.nuked(t))
			got, gErr := f.d.Get(f.task.ID)
			testutil.NoError(t, gErr)
			testutil.Equal(t, got.Status, model.StatusInProgress)
			testutil.False(t, got.Archived)
			_, sErr := os.Stat(f.task.Worktree)
			testutil.NoError(t, sErr)
			testutil.Contains(t, logs.String(), "host busy (repo cleanup lock")
		})
	}
}

// The repo lock is held from before the stop through the recheck and the
// local removal, and released before followUp runs.
func TestNukeResidentWorkerChecked_RepoLockHeldAcrossStop(t *testing.T) {
	f := newRepoLockFix(t)
	heldAt := func(what string) {
		if unlock, ok := agent.TryLockRepo(f.repo); ok {
			unlock()
			t.Errorf("repo lock not held at %s", what)
		}
	}
	f.runner.onStop = func(string) { heldAt("the stop") }
	f.opts.Recheck = func(*model.Task) error {
		*f.events = append(*f.events, "recheck")
		heldAt("the recheck")
		return nil
	}
	f.opts.MutateBy = f.clk.Now().Add(time.Minute)
	// reclaimResidentWorkerTask probes session presence just before and just
	// after the local removal; both probes must still see the lock held.
	probes := 0
	runner := &probingNukeRunner{
		recordingNukeRunner: f.runner,
		probeErr: func(bool) error {
			if slices.Contains(*f.events, "recheck") {
				probes++
				heldAt("local removal")
			}
			return nil
		},
	}
	followUp, err := NukeResidentWorkerChecked(f.d, runner, f.view, f.opts)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, *f.events, []string{"stop", "recheck"})
	testutil.True(t, probes >= 2)
	testutil.True(t, f.nuked(t))
	unlock, ok := agent.TryLockRepo(f.repo)
	testutil.True(t, ok) // released before followUp
	unlock()
	followUp()
}
