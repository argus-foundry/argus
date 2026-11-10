package hera

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// fastStopWait keeps the checked delete's exit wait short in tests.
var fastStopWait = StopWait{Max: 2 * time.Second, Settle: time.Millisecond, Poll: time.Millisecond}

// recordingNukeRunner records Stop calls and the order of events relative to
// the recheck. onStop runs inside Stop (a simulated exit hook); keepAlive makes
// Stop leave the session present (a session that ignores the stop).
type recordingNukeRunner struct {
	mu        sync.Mutex
	live      map[string]bool
	stopErr   error
	keepAlive bool
	onStop    func(id string)
	stoppedAt time.Time
	events    *[]string
}

func (r *recordingNukeRunner) HasSession(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.live[id]
}

func (r *recordingNukeRunner) Stop(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.events = append(*r.events, "stop")
	r.stoppedAt = time.Now()
	if errors.Is(r.stopErr, agent.ErrSessionNotFound) {
		delete(r.live, id) // the session had already exited
	}
	if r.stopErr != nil {
		return r.stopErr
	}
	if r.onStop != nil {
		r.onStop(id)
	}
	if !r.keepAlive {
		delete(r.live, id)
	}
	return nil
}

func TestNukeResidentWorkerChecked(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stopErr    error
		keepAlive  bool
		recheckErr error
		wantNuked  bool
		wantEvents []string
		wantErr    error
	}{
		{"recheck passes", nil, false, nil, true, []string{"stop", "recheck"}, nil},
		// The post-stop refusal keeps everything except the stopped session.
		{"recheck refuses", nil, false, errors.New("dirty"), false, []string{"stop", "recheck"}, nil},
		// A session that is already gone is not a stop failure.
		{"session already gone", agent.ErrSessionNotFound, false, nil, true, []string{"stop", "recheck"}, nil},
		// Any other stop failure refuses before the recheck and the nuke, once
		// the exit wait has decided whether the stop took.
		{"stop fails", errors.New("rpc down"), false, nil, false, []string{"stop"}, ErrWorkerStopFailed},
		// A session still present after the exit wait refuses before the
		// recheck: it could keep writing after it.
		{"session never exits", nil, true, nil, false, []string{"stop"}, ErrWorkerSessionStillRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
			d, _, view := residentWorkerNukeFixture(t, task)
			var events []string
			runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, stopErr: tc.stopErr, keepAlive: tc.keepAlive, events: &events}
			recheck := func(got *model.Task) error {
				events = append(events, "recheck")
				// The role is still un-nuked when the recheck runs.
				r, err := d.HeraRole(view.RoleID)
				testutil.NoError(t, err)
				testutil.True(t, r.NukedAt == nil)
				testutil.Equal(t, got.ID, task.ID)
				return tc.recheckErr
			}
			wait := fastStopWait
			if tc.keepAlive || tc.stopErr != nil {
				wait.Max = 30 * time.Millisecond
			}
			_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: recheck, Wait: wait})
			testutil.Equal(t, err == nil, tc.wantNuked)
			if tc.recheckErr != nil {
				testutil.ErrorIs(t, err, tc.recheckErr)
			}
			if tc.wantErr != nil {
				testutil.ErrorIs(t, err, tc.wantErr)
			}
			if len(events) < len(tc.wantEvents) {
				t.Fatalf("events = %v, want prefix %v", events, tc.wantEvents)
			}
			testutil.DeepEqual(t, events[:len(tc.wantEvents)], tc.wantEvents)
			if !tc.wantNuked {
				testutil.DeepEqual(t, events, tc.wantEvents)
			}
			r, err := d.HeraRole(view.RoleID)
			testutil.NoError(t, err)
			testutil.Equal(t, r.NukedAt != nil, tc.wantNuked)
			got, err := d.Get(task.ID)
			if tc.wantNuked {
				return
			}
			testutil.NoError(t, err)
			testutil.False(t, got.Archived)
			live, err := d.HeraLiveBindingByTask(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, live.RoleID, view.RoleID)
		})
	}
}

// The recheck never runs sooner than Settle after the stop, even when the
// session is gone at once: process-group children (a background job trapping
// SIGTERM) can keep writing until the group SIGKILL.
func TestNukeResidentWorkerChecked_RecheckWaitsForSettle(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	var events []string
	runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}
	const settle = 80 * time.Millisecond
	var gap time.Duration
	recheck := func(*model.Task) error {
		runner.mu.Lock()
		gap = time.Since(runner.stoppedAt)
		runner.mu.Unlock()
		return errors.New("refuse")
	}
	_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: recheck, Wait: StopWait{Max: 2 * time.Second, Settle: settle, Poll: time.Millisecond}})
	if err == nil {
		t.Fatal("recheck refusal must refuse the delete")
	}
	if gap < settle {
		t.Fatalf("recheck ran %s after the stop, want >= %s", gap, settle)
	}
}

// The delete's own stop must not close the worker out: the exit hook
// (RollHeraWorkerToReview) would stamp ready_to_close and the refused worker
// would look finished and refuse revive.
func TestNukeResidentWorkerChecked_RefusalLeavesWorkerResumable(t *testing.T) {
	for _, tc := range []struct {
		name string
		// hookOnStop runs the daemon exit hook inside Stop (in-process runner:
		// the hook runs before the session leaves the map).
		hookOnStop bool
		// lateHook runs the exit hook after the call returned (supervisor exit
		// relay racing the delete).
		lateHook  bool
		keepAlive bool
		// stampOnStop stamps ready_to_close during the stop regardless of
		// status (an exit-hook site the pre-stop flip does not cover).
		stampOnStop bool
	}{
		{"hook during stop", true, false, false, false},
		{"hook after return", false, true, false, false},
		{"exit wait timeout then hook", false, true, true, false},
		{"stamp during stop", false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
			d, _, view := residentWorkerNukeFixture(t, task)
			var events []string
			runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, keepAlive: tc.keepAlive, events: &events}
			runner.onStop = func(id string) {
				if tc.hookOnStop {
					_, err := d.RollHeraWorkerToReview(id)
					testutil.NoError(t, err)
				}
				if tc.stampOnStop {
					testutil.NoError(t, d.SetMeta(id, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				}
			}
			wait := fastStopWait
			if tc.keepAlive {
				wait.Max = 30 * time.Millisecond
			}
			_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: func(*model.Task) error { return errors.New("new work") }, Wait: wait})
			if err == nil {
				t.Fatal("refusal expected")
			}
			if tc.lateHook {
				_, err := d.RollHeraWorkerToReview(task.ID)
				testutil.NoError(t, err)
			}
			awaiting, err := d.HeraWorkerAwaitingCloseout(task.ID)
			testutil.NoError(t, err)
			testutil.False(t, awaiting)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Status, model.StatusInReview)
			// Resumable: the revive helper restores it.
			revived, err := d.ReviveHeraWorkerToInProgress(task.ID)
			testutil.NoError(t, err)
			testutil.True(t, revived)
		})
	}
}

// A stamp the worker already carried before the delete is not the delete's
// to clear.
func TestNukeResidentWorkerChecked_RefusalKeepsPriorCloseout(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	flipped, err := d.RollHeraWorkerToReview(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, flipped)
	var events []string
	runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}
	_, err = NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: func(*model.Task) error { return errors.New("new work") }, Wait: fastStopWait})
	if err == nil {
		t.Fatal("refusal expected")
	}
	awaiting, err := d.HeraWorkerAwaitingCloseout(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, awaiting)
}

// A failed Stop that a successful probe after the exit wait shows did not
// take leaves the session running, so the pre-stop in_review flip is reverted.
func TestNukeResidentWorkerChecked_StopFailureRevertsFlip(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	var events []string
	runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, stopErr: errors.New("rpc down"), events: &events}
	_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: func(*model.Task) error { return nil }, Wait: StopWait{Max: 30 * time.Millisecond, Settle: time.Millisecond, Poll: time.Millisecond}})
	testutil.ErrorIs(t, err, ErrWorkerStopFailed)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)
}

// A task kept for another live binding is not reclaimed, so the checked
// delete neither stops its session nor runs the recheck.
func TestNukeResidentWorkerChecked_SecondBindingSkipsStopAndRecheck(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	o2, err := d.CreateHeraOrchestrator("other", "")
	testutil.NoError(t, err)
	r2, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o2.ID, Name: "w2", Kind: db.HeraKindWorker, ArgusProject: "p"})
	testutil.NoError(t, err)
	_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: r2.ID, OrchestratorID: o2.ID, ArgusTaskID: task.ID})
	testutil.NoError(t, err)
	var events []string
	runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}
	recheck := func(*model.Task) error {
		events = append(events, "recheck")
		return errors.New("must not run")
	}
	_, err = NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{Recheck: recheck, Wait: fastStopWait})
	testutil.NoError(t, err)
	testutil.Equal(t, len(events), 0)
	testutil.True(t, runner.HasSession(task.ID))
	role, err := d.HeraRole(view.RoleID)
	testutil.NoError(t, err)
	testutil.True(t, role.NukedAt != nil)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)
}

// With no recheck the resident delete keeps its original order (force path):
// no stop happens before the nuke.
func TestNukeResidentWorkerChecked_NilRecheckUnchanged(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	var events []string
	runner := &recordingNukeRunner{live: map[string]bool{task.ID: true}, events: &events}
	_, err := NukeResidentWorkerChecked(d, runner, view, ResidentDeleteOptions{})
	testutil.NoError(t, err)
	r, err := d.HeraRole(view.RoleID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt != nil)
	testutil.DeepEqual(t, events, []string{"stop"}) // the reclaim's own stop only
}

func TestStopWaitDefaults(t *testing.T) {
	testutil.Equal(t, StopWait{}.withDefaults(), DefaultResidentStopWait)
	got := StopWait{Max: time.Second, Settle: 5 * time.Second}.withDefaults()
	testutil.Equal(t, got.Settle, time.Second)
	testutil.Equal(t, got.Poll, DefaultResidentStopWait.Poll)
	// Settle must outlast agent's 5s process-group SIGKILL escalation.
	testutil.True(t, DefaultResidentStopWait.Settle > 5*time.Second)
	testutil.True(t, DefaultResidentStopWait.Settle <= DefaultResidentStopWait.Max)
	// Settle runs from when Stop returns; Max from when it began. A Stop RPC
	// up to the supervisor client's 2s timeout must still settle within Max.
	testutil.True(t, DefaultResidentStopWait.Settle+2*time.Second <= DefaultResidentStopWait.Max)
}
