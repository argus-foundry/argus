package hera

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// fakeStopClock is a virtual clock: Sleep advances it, and so can a runner
// whose Stop "takes" time.
type fakeStopClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeStopClock() *fakeStopClock { return &fakeStopClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeStopClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeStopClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *fakeStopClock) clock() Clock { return Clock{NowFn: c.Now, SleepFn: c.Sleep} }

// probingNukeRunner is a supervisor-client-like runner: SessionPresent can
// fail (probeErr), and HasSession then folds the failure into "no session"
// exactly like client.Client.HasSession does.
type probingNukeRunner struct {
	*recordingNukeRunner
	mu       sync.Mutex
	stopped  bool
	probeErr func(stopped bool) error
}

func (r *probingNukeRunner) err() error {
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if r.probeErr == nil {
		return nil
	}
	return r.probeErr(stopped)
}

func (r *probingNukeRunner) SessionPresent(_ context.Context, id string) (bool, error) {
	if err := r.err(); err != nil {
		return false, err
	}
	return r.recordingNukeRunner.HasSession(id), nil
}

func (r *probingNukeRunner) HasSession(id string) bool {
	if r.err() != nil {
		return false
	}
	return r.recordingNukeRunner.HasSession(id)
}

func (r *probingNukeRunner) Stop(id string) error {
	err := r.recordingNukeRunner.Stop(id)
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	return err
}

var errProbe = errors.New("daemon RPC call timed out")

// An unreadable session presence counts as present: it never skips the stop
// (initial probe) and never ends the exit wait early (probe after the stop).
func TestNukeResidentWorkerChecked_ProbeErrorCountsAsPresent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		probeErr   func(stopped bool) error
		keepAlive  bool
		wantNuked  bool
		wantEvents []string
	}{
		// Old code: HasSession read false, the stop was skipped and the
		// recheck ran against a live session.
		{"initial probe fails", func(stopped bool) error {
			if !stopped {
				return errProbe
			}
			return nil
		}, false, true, []string{"stop", "recheck"}},
		// Old code: the poll read "gone" and the recheck + nuke ran while
		// the session was still alive.
		{"probe fails after stop", func(stopped bool) error {
			if stopped {
				return errProbe
			}
			return nil
		}, true, false, []string{"stop"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
			d, _, view := residentWorkerNukeFixture(t, task)
			var events []string
			runner := &probingNukeRunner{
				recordingNukeRunner: &recordingNukeRunner{live: map[string]bool{task.ID: true}, keepAlive: tc.keepAlive, events: &events},
				probeErr:            tc.probeErr,
			}
			recheck := func(*model.Task) error {
				events = append(events, "recheck")
				return nil
			}
			_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: recheck,
				Wait: StopWait{Max: 30 * time.Millisecond, Settle: time.Millisecond, Poll: time.Millisecond}})
			testutil.DeepEqual(t, events, tc.wantEvents)
			r, rErr := d.HeraRole(view.RoleID)
			testutil.NoError(t, rErr)
			testutil.Equal(t, r.NukedAt != nil, tc.wantNuked)
			if tc.wantNuked {
				testutil.NoError(t, err)
				return
			}
			testutil.ErrorIs(t, err, ErrWorkerSessionStillRunning)
			testutil.Contains(t, err.Error(), "unreadable")
			got, gErr := d.Get(task.ID)
			testutil.NoError(t, gErr)
			// Treated as stopped: the flip is kept, never stamped.
			testutil.Equal(t, got.Status, model.StatusInReview)
			awaiting, aErr := d.HeraWorkerAwaitingCloseout(task.ID)
			testutil.NoError(t, aErr)
			testutil.False(t, awaiting)
		})
	}
}

// slowStopRunner's Stop takes stopTakes of fake-clock time (a slow RPC); the
// session is gone as soon as Stop returns.
type slowStopRunner struct {
	*recordingNukeRunner
	clk       *fakeStopClock
	stopTakes time.Duration
	returned  time.Time
}

func (r *slowStopRunner) Stop(id string) error {
	r.clk.Sleep(r.stopTakes)
	err := r.recordingNukeRunner.Stop(id)
	r.returned = r.clk.Now()
	return err
}

// Settle runs from when Stop RETURNS: the supervisor starts its group-SIGKILL
// timer only when its own stop runs, so time spent in the RPC must not count.
func TestNukeResidentWorkerChecked_SettleMeasuredFromStopReturn(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	var events []string
	clk := newFakeStopClock()
	runner := &slowStopRunner{recordingNukeRunner: &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}, clk: clk, stopTakes: 1500 * time.Millisecond}
	var recheckAt time.Time
	recheck := func(*model.Task) error {
		recheckAt = clk.Now()
		return nil
	}
	_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: recheck, Clock: clk.clock(),
		Wait: StopWait{Max: 10 * time.Second, Settle: 6 * time.Second, Poll: 100 * time.Millisecond}})
	testutil.NoError(t, err)
	if gap := recheckAt.Sub(runner.returned); gap < 6*time.Second {
		t.Fatalf("recheck ran %s after Stop returned, want >= 6s (old code measured from before the 1.5s RPC: 4.5s)", gap)
	}
}

// A failed Stop is not trusted: the delete waits out the exit wait and only
// reverts the pre-stop flip when a successful probe shows the session still
// running. It is always refused.
func TestNukeResidentWorkerChecked_StopErrorOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		// exits: the (timed-out) stop was delivered and the session exits.
		exits      bool
		probeErr   bool
		wantStatus model.Status
		wantText   string
	}{
		{"stop did not take", false, false, model.StatusInProgress, "still running"},
		{"stop delivered, session exited", true, false, model.StatusInReview, "has since exited"},
		{"session state unreadable", false, true, model.StatusInReview, "unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
			d, _, view := residentWorkerNukeFixture(t, task)
			var events []string
			inner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, stopErr: errProbe, events: &events}
			runner := &probingNukeRunner{recordingNukeRunner: inner}
			if tc.exits {
				runner.probeErr = func(stopped bool) error {
					if stopped {
						inner.mu.Lock()
						delete(inner.live, task.ID)
						inner.mu.Unlock()
					}
					return nil
				}
			}
			if tc.probeErr {
				runner.probeErr = func(stopped bool) error {
					if stopped {
						return errProbe
					}
					return nil
				}
			}
			clk := newFakeStopClock()
			recheck := func(*model.Task) error {
				events = append(events, "recheck")
				return nil
			}
			_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: recheck, Clock: clk.clock(),
				Wait: StopWait{Max: 10 * time.Second, Settle: 6 * time.Second, Poll: 100 * time.Millisecond}})
			testutil.ErrorIs(t, err, ErrWorkerStopFailed)
			testutil.Contains(t, err.Error(), tc.wantText)
			testutil.DeepEqual(t, events, []string{"stop"})
			// The supervisor's exit relay lands late; it must not close the
			// worker out when the flip was kept.
			if tc.exits {
				_, hErr := d.RollHeraWorkerToReview(task.ID)
				testutil.NoError(t, hErr)
			}
			got, gErr := d.Get(task.ID)
			testutil.NoError(t, gErr)
			testutil.Equal(t, got.Status, tc.wantStatus)
			awaiting, aErr := d.HeraWorkerAwaitingCloseout(task.ID)
			testutil.NoError(t, aErr)
			testutil.False(t, awaiting)
			r, rErr := d.HeraRole(view.RoleID)
			testutil.NoError(t, rErr)
			testutil.True(t, r.NukedAt == nil)
		})
	}
}

// Past MutateBy the delete refuses before its first change: no flip, no stop,
// no nuke.
func TestNukeResidentWorkerChecked_HostBusyChangesNothing(t *testing.T) {
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
			clk := newFakeStopClock()
			_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: tc.recheck, Clock: clk.clock(), MutateBy: clk.Now().Add(-time.Millisecond)})
			testutil.ErrorIs(t, err, ErrHostBusy)
			testutil.Equal(t, len(events), 0)
			got, gErr := d.Get(task.ID)
			testutil.NoError(t, gErr)
			testutil.Equal(t, got.Status, model.StatusInProgress)
			testutil.False(t, got.Archived)
			r, rErr := d.HeraRole(view.RoleID)
			testutil.NoError(t, rErr)
			testutil.True(t, r.NukedAt == nil)
			_, bErr := d.HeraLiveBindingByTask(task.ID)
			testutil.NoError(t, bErr)
		})
	}
}

// GiveUpBy caps the exit wait below Max so the recheck and local nuke still
// fit the caller's deadline.
func TestNukeResidentWorkerChecked_GiveUpByCapsWait(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	var events []string
	runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, keepAlive: true, events: &events}
	clk := newFakeStopClock()
	start := clk.Now()
	_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: func(*model.Task) error { return nil }, Clock: clk.clock(),
		GiveUpBy: start.Add(time.Second), Wait: StopWait{Max: 10 * time.Second, Settle: 6 * time.Second, Poll: 100 * time.Millisecond}})
	testutil.ErrorIs(t, err, ErrWorkerSessionStillRunning)
	if waited := clk.Now().Sub(start); waited > 1200*time.Millisecond {
		t.Fatalf("exit wait ran %s, want capped near 1s by GiveUpBy", waited)
	}
}

// The origin branch delete runs from followUp with its own timeout; a nil
// target is skipped.
func TestResidentRemoteBranchDelete(t *testing.T) {
	var gotDir, gotBranch string
	var hadDeadline bool
	opts := ResidentDeleteOptions{RemoteBranchTimeout: time.Minute, RemoteBranchDelete: func(ctx context.Context, dir, branch string) error {
		_, hadDeadline = ctx.Deadline()
		gotDir, gotBranch = dir, branch
		return nil
	}}
	residentRemoteBranchDelete(opts, "w", "/repo", "argus/x")
	testutil.Equal(t, gotDir, "/repo")
	testutil.Equal(t, gotBranch, "argus/x")
	testutil.True(t, hadDeadline)
	gotBranch = ""
	residentRemoteBranchDelete(opts, "w", "/repo", "")
	testutil.Equal(t, gotBranch, "")
	opts.RemoteBranchDelete = func(context.Context, string, string) error { return errors.New("offline") }
	residentRemoteBranchDelete(opts, "w", "/repo", "argus/x") // logged, never panics
}
