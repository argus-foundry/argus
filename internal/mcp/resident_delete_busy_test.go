package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// busyFakeSession is a session handle whose liveness and idleness the test
// controls. Every other SessionHandle method panics (nil embedded interface):
// the delete path must not need them.
type busyFakeSession struct {
	agent.SessionHandle
	mu    sync.Mutex
	alive bool
	idle  bool
	// tail, when set, is the session's recent output for the content-aware
	// idle check; nil reads as no output.
	tail func() []byte
}

func (s *busyFakeSession) Alive() bool  { s.mu.Lock(); defer s.mu.Unlock(); return s.alive }
func (s *busyFakeSession) IsIdle() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.idle }
func (s *busyFakeSession) PTYSize() (int, int) {
	return 80, 24
}
func (s *busyFakeSession) RecentOutputTail(n int) []byte {
	if s.tail == nil {
		return nil
	}
	b := s.tail()
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return b
}

// The production values, captured before init() shortens them: package-level
// var initializers all run before any init function.
var (
	prodStopWait     = residentStopWait
	prodIdleSampling = residentIdleSampling
)

// Tests in this package never wait for real stop settling or idle sampling.
func init() {
	residentStopWait = hera.StopWait{Max: 2 * time.Second, Settle: time.Millisecond, Poll: time.Millisecond}
	residentIdleSampling = residentIdleSamplingCfg{Window: 20 * time.Millisecond, Interval: 5 * time.Millisecond}
}

// fakeResidentClock is a virtual residentClock: Sleep advances it, so a
// multi-second window costs nothing. Now calls are counted so a test can
// tell the handler has started.
type fakeResidentClock struct {
	mu    sync.Mutex
	t     time.Time
	calls int
}

func (c *fakeResidentClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.t
}

func (c *fakeResidentClock) Sleep(d time.Duration) { c.Advance(d) }

func (c *fakeResidentClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *fakeResidentClock) nowCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// useFakeResidentClock installs a fake residentClock for one test.
func useFakeResidentClock(t *testing.T) *fakeResidentClock {
	t.Helper()
	old := residentClock
	t.Cleanup(func() { residentClock = old })
	c := &fakeResidentClock{t: time.Unix(1_700_000_000, 0)}
	residentClock = hera.Clock{NowFn: c.Now, SleepFn: c.Sleep}
	return c
}

// fakeIdleClock runs residentIdleSampling with window/interval on a virtual
// clock.
func fakeIdleClock(t *testing.T, window, interval time.Duration) {
	t.Helper()
	old := residentIdleSampling
	t.Cleanup(func() { residentIdleSampling = old })
	residentIdleSampling = residentIdleSamplingCfg{Window: window, Interval: interval}
	useFakeResidentClock(t)
}

// busyFakeRunner is a NukeRunner + session reader over busyFakeSessions.
type busyFakeRunner struct {
	mu       sync.Mutex
	sessions map[string]*busyFakeSession
	stops    []string
	pending  bool // HasPendingRestart
	// onStop runs inside Stop (a simulated exit hook); keepAlive makes Stop
	// leave the session present (a session that does not exit).
	onStop    func(taskID string)
	keepAlive bool
	noHandle  bool // Get returns nil even while HasSession is true
	// probeErr makes SessionPresent fail; HasSession then folds the failure
	// into "no session", exactly like the supervisor client.
	probeErr error
}

func (r *busyFakeRunner) SessionPresent(_ context.Context, taskID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.probeErr != nil {
		return false, r.probeErr
	}
	return r.sessions[taskID] != nil, nil
}

func (r *busyFakeRunner) Get(taskID string) agent.SessionHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.noHandle {
		return nil
	}
	if s := r.sessions[taskID]; s != nil {
		return s
	}
	return nil
}

func (r *busyFakeRunner) HasSession(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.probeErr == nil && r.sessions[taskID] != nil
}

func (r *busyFakeRunner) HasPendingRestart(string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending
}

func (r *busyFakeRunner) Stop(taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sessions[taskID]
	if s == nil {
		return agent.ErrSessionNotFound
	}
	r.stops = append(r.stops, taskID)
	if r.onStop != nil {
		r.onStop(taskID)
	}
	if r.keepAlive {
		return nil
	}
	s.mu.Lock()
	s.alive = false
	s.mu.Unlock()
	delete(r.sessions, taskID)
	return nil
}

func (r *busyFakeRunner) stopCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.stops)
}

// busyDeleteFixture seeds a resident worker "work" in a real git worktree
// with a session in the given state, served by a fake runner.
func busyDeleteFixture(t *testing.T, idle bool) (*Server, *db.DB, *model.Task, *db.HeraRole, *busyFakeRunner, string) {
	t.Helper()
	s, d, res, o := residentHeraFixture(t)
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(repo, "init", "-b", "master")
	run(repo, "config", "user.name", "Test")
	run(repo, "config", "user.email", "test@example.com")
	run(repo, "config", "commit.gpgsign", "false")
	run(repo, "commit", "--allow-empty", "-m", "initial")
	branch := "argus/resident/work"
	wt := filepath.Join(db.DataDir(), "worktrees", "p", "work")
	run(repo, "worktree", "add", "-b", branch, wt)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: repo, Branch: "master"}))
	task := &model.Task{ID: "work", Name: "work", Project: "p", Branch: branch, BaseBranch: "master", Worktree: wt, Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	role, _, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker}, task.ID, wt)
	testutil.NoError(t, err)
	runner := &busyFakeRunner{sessions: map[string]*busyFakeSession{task.ID: {alive: true, idle: idle}}}
	s.taskStopper = runner
	return s, d, task, role, runner, res.ID
}

func TestResidentDelete_BusyWorkerRefused(t *testing.T) {
	s, d, task, role, runner, caller := busyDeleteFixture(t, false)
	classified := false
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		classified = true
		return mergesafety.Verdict{Safe: true}, nil
	}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "worker is busy; wait, or force")
	testutil.False(t, classified)
	testutil.Equal(t, runner.stopCount(), 0)
	r, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt == nil)
	b, err := d.HeraLiveBindingByRole(role.ID)
	if err != nil {
		t.Fatalf("binding not live: %v", err)
	}
	testutil.Equal(t, b.ArgusTaskID, task.ID)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, got.Archived)
	_, err = os.Stat(task.Worktree)
	testutil.NoError(t, err)
}

func TestResidentDelete_UnreadableSessionStateRefused(t *testing.T) {
	s, d, task, role, _, caller := busyDeleteFixture(t, true)
	// A runner that can stop but cannot report session state fails closed.
	s.taskStopper = &mockNukeOnlyRunner{}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "worker is busy; wait, or force")
	r, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt == nil)
	_, err = os.Stat(task.Worktree)
	testutil.NoError(t, err)
}

type mockNukeOnlyRunner struct{}

func (mockNukeOnlyRunner) HasSession(string) bool { return true }
func (mockNukeOnlyRunner) Stop(string) error      { return nil }

// Work written while the delete classifies (the session is still live then)
// is caught by the re-check after the stop and is never destroyed.
func TestResidentDelete_WorkWrittenDuringClassificationKept(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(t *testing.T, wt string)
		want  []string
	}{
		{"dirty file", func(t *testing.T, wt string) {
			testutil.NoError(t, os.WriteFile(filepath.Join(wt, "late-notes.md"), []byte("work"), 0600))
		}, []string{"dirty files: 1", `"late-notes.md"`}},
		{"new commit", func(t *testing.T, wt string) {
			cmd := exec.Command("git", "commit", "--allow-empty", "-m", "late commit")
			cmd.Dir = wt
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git commit: %v: %s", err, out)
			}
		}, []string{"moved while the delete was classifying"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, task, role, runner, caller := busyDeleteFixture(t, true)
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				tc.write(t, task.Worktree)
				return mergesafety.Verdict{Safe: true}, nil
			}
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
			testutil.True(t, isErr)
			for _, part := range append([]string{"not deleted", "session was stopped"}, tc.want...) {
				testutil.Contains(t, text, part)
			}
			// The one side effect: the session is stopped.
			testutil.Equal(t, runner.stopCount(), 1)
			testutil.False(t, runner.HasSession(task.ID))
			r, err := d.HeraRole(role.ID)
			testutil.NoError(t, err)
			testutil.True(t, r.NukedAt == nil)
			b, err := d.HeraLiveBindingByRole(role.ID)
			if err != nil {
				t.Fatalf("binding not live: %v", err)
			}
			testutil.Equal(t, b.ArgusTaskID, task.ID)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.False(t, got.Archived)
			_, err = os.Stat(task.Worktree)
			testutil.NoError(t, err)
			if tc.name == "dirty file" {
				_, err = os.Stat(filepath.Join(task.Worktree, "late-notes.md"))
				testutil.NoError(t, err)
			}
		})
	}
}

func TestResidentDelete_IdleCleanAndForceBusyStillDelete(t *testing.T) {
	for _, tc := range []struct {
		name  string
		idle  bool
		force bool
	}{
		{"idle clean", true, false},
		{"force busy dirty", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, task, role, runner, caller := busyDeleteFixture(t, tc.idle)
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.Verdict{Safe: true}, nil
			}
			if tc.force {
				testutil.NoError(t, os.WriteFile(filepath.Join(task.Worktree, "dirty"), []byte("x"), 0600))
			}
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", fmt.Sprintf(`{"role":"work","force":%t}`, tc.force))
			testutil.False(t, isErr)
			testutil.Contains(t, text, "Deleted worker work")
			if tc.force {
				testutil.Contains(t, text, `"dirty_count": "1"`)
			}
			testutil.Equal(t, runner.stopCount(), 1)
			r, err := d.HeraRole(role.ID)
			testutil.NoError(t, err)
			testutil.True(t, r.NukedAt != nil)
			_, err = os.Stat(task.Worktree)
			testutil.True(t, os.IsNotExist(err))
			_, err = d.Get(task.ID)
			testutil.ErrorIs(t, err, db.ErrTaskNotFound)
		})
	}
}

// assertWorkerKept checks a refused delete left the role, binding, task and
// worktree in place.
func assertWorkerKept(t *testing.T, d *db.DB, task *model.Task, role *db.HeraRole) {
	t.Helper()
	r, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt == nil)
	b, err := d.HeraLiveBindingByRole(role.ID)
	if err != nil {
		t.Fatalf("binding not live: %v", err)
	}
	testutil.Equal(t, b.ArgusTaskID, task.ID)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, got.Archived)
	_, err = os.Stat(task.Worktree)
	testutil.NoError(t, err)
}

func TestResidentDelete_SessionStateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(r *busyFakeRunner)
		want string
	}{
		// An idle session with a queued restart is about to come back.
		{"pending restart", func(r *busyFakeRunner) { r.pending = true }, "a session restart is pending"},
		// A session the runner reports but cannot hand over is unreadable.
		{"no handle but session", func(r *busyFakeRunner) { r.noHandle = true }, "cannot read the worker's session state"},
		// A presence probe that fails (supervisor RPC timeout) is not
		// "absent": HasSession alone would have read false and let the
		// delete proceed against a possibly busy worker.
		{"presence unreadable", func(r *busyFakeRunner) {
			r.noHandle = true
			r.probeErr = errors.New("daemon RPC call timed out")
		}, "cannot read the worker's session state: daemon RPC call timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, task, role, runner, caller := busyDeleteFixture(t, true)
			tc.set(runner)
			classified := false
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				classified = true
				return mergesafety.Verdict{Safe: true}, nil
			}
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
			testutil.True(t, isErr)
			testutil.Contains(t, text, "worker is busy; wait, or force")
			testutil.Contains(t, text, tc.want)
			testutil.False(t, classified)
			testutil.Equal(t, runner.stopCount(), 0)
			assertWorkerKept(t, d, task, role)
		})
	}
}

// A dead handle (exited, not yet reaped) is absent, not busy.
func TestResidentDelete_DeadHandleDeletes(t *testing.T) {
	s, d, task, role, runner, caller := busyDeleteFixture(t, false)
	runner.sessions[task.ID].alive = false
	// A dead handle is absent outright: it is never content-sampled.
	runner.sessions[task.ID].tail = func() []byte {
		t.Error("dead handle must not be content-sampled")
		return nil
	}
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		return mergesafety.Verdict{Safe: true}, nil
	}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.False(t, isErr)
	testutil.Contains(t, text, "Deleted worker work")
	r, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt != nil)
}

// A session whose raw PTY output never goes quiet but whose screen is a
// stable parked prompt is idle (BUG-083); one whose content keeps changing is
// busy.
func TestResidentDelete_ContentAwareIdle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tail    func() func() []byte
		deleted bool
	}{
		{"stable parked prompt", func() func() []byte {
			return func() []byte {
				return []byte("Edit file foo.go?\r\nDo you want to proceed?\r\n> 1. Yes\r\n  2. No\r\n")
			}
		}, true},
		{"changing content", func() func() []byte {
			n := 0
			return func() []byte {
				n++
				return []byte(fmt.Sprintf("compiling package %s\r\n", strings.Repeat("x", n)))
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The production sampling, on a virtual clock.
			fakeIdleClock(t, prodIdleSampling.Window, prodIdleSampling.Interval)
			s, d, task, role, runner, caller := busyDeleteFixture(t, false)
			runner.sessions[task.ID].tail = tc.tail()
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.Verdict{Safe: true}, nil
			}
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
			if !tc.deleted {
				testutil.True(t, isErr)
				testutil.Contains(t, text, "worker is busy; wait, or force")
				testutil.Equal(t, runner.stopCount(), 0)
				assertWorkerKept(t, d, task, role)
				return
			}
			testutil.False(t, isErr)
			testutil.Contains(t, text, "Deleted worker work")
			r, err := d.HeraRole(role.ID)
			testutil.NoError(t, err)
			testutil.True(t, r.NukedAt != nil)
		})
	}
}

// The branch tip is captured before the summary: a commit landing after the
// summary (stale and empty, so an unsafe verdict alone would not refuse) moves
// the tip, and the post-stop re-check refuses with the commit kept.
func TestResidentDelete_CommitAfterSummaryKept(t *testing.T) {
	s, d, task, role, runner, caller := busyDeleteFixture(t, true)
	old := residentSummarize
	t.Cleanup(func() { residentSummarize = old })
	residentSummarize = func(ctx context.Context, cfg config.Config, tk *model.Task, pr string) mergesafety.WorkSummary {
		sum := old(ctx, cfg, tk, pr)
		cmd := exec.Command("git", "commit", "--allow-empty", "-m", "landed after summary")
		cmd.Dir = tk.Worktree
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v: %s", err, out)
		}
		return sum
	}
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		return mergesafety.Verdict{Reason: "unmerged commits"}, nil
	}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "moved while the delete was classifying")
	testutil.Contains(t, text, "can be resumed")
	testutil.Equal(t, runner.stopCount(), 1)
	assertWorkerKept(t, d, task, role)
	cmd := exec.Command("git", "log", "-1", "--format=%s", task.Branch)
	cmd.Dir = task.Worktree
	out, err := cmd.Output()
	testutil.NoError(t, err)
	testutil.Equal(t, strings.TrimSpace(string(out)), "landed after summary")
}

// A refused delete must not leave the worker closed out by its own stop's
// exit hook.
func TestResidentDelete_RefusalLeavesWorkerResumable(t *testing.T) {
	s, d, task, role, runner, caller := busyDeleteFixture(t, true)
	runner.onStop = func(id string) {
		// The daemon's exit hook for a stopped hera worker.
		_, err := d.RollHeraWorkerToReview(id)
		testutil.NoError(t, err)
	}
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		testutil.NoError(t, os.WriteFile(filepath.Join(task.Worktree, "late.md"), []byte("w"), 0600))
		return mergesafety.Verdict{Safe: true}, nil
	}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "not deleted")
	assertWorkerKept(t, d, task, role)
	awaiting, err := d.HeraWorkerAwaitingCloseout(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, awaiting)
}

// The stop wait timing out refuses: the session may still be writing.
func TestResidentDelete_SessionNotExitingRefused(t *testing.T) {
	old := residentStopWait
	t.Cleanup(func() { residentStopWait = old })
	residentStopWait = hera.StopWait{Max: 30 * time.Millisecond, Settle: time.Millisecond, Poll: time.Millisecond}
	s, d, task, role, runner, caller := busyDeleteFixture(t, true)
	runner.keepAlive = true
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		return mergesafety.Verdict{Safe: true}, nil
	}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "not deleted")
	testutil.Contains(t, text, "did not exit after stop")
	assertWorkerKept(t, d, task, role)
}

// The production values (init() shortens them for every other test here).
func TestResidentDeleteProductionValues(t *testing.T) {
	testutil.Equal(t, prodStopWait, hera.DefaultResidentStopWait)
	testutil.Equal(t, prodIdleSampling.Window, residentIdleWindow)
	// Longer than agent's 3s content-stability threshold, and enough ticks
	// for the parked-selection escalation.
	testutil.True(t, prodIdleSampling.Window > 3*time.Second)
	testutil.True(t, int(prodIdleSampling.Window/prodIdleSampling.Interval) >= agent.NeedsInputEscalationTicks)
}

// The overall deadline fits the 60s MCP client deadline with headroom, the
// post-stop remainder fits inside it, and a call that did not queue still has
// room for a full idle sample plus classification before the stop.
func TestResidentDeleteBudget(t *testing.T) {
	testutil.True(t, residentDeleteDeadline <= 50*time.Second)
	postStop := prodStopWait.Max + residentRecheckBudget + residentLocalNukeReserve
	preStop := residentDeleteDeadline - postStop
	testutil.True(t, preStop >= residentIdleWindow+5*time.Second)
	testutil.True(t, residentForceReserve < residentDeleteDeadline-5*time.Second)
	// The exit wait's GiveUpBy (deadline minus re-check and local nuke) never
	// cuts a full Max short when the stop began on time.
	testutil.True(t, residentDeleteDeadline-(residentRecheckBudget+residentLocalNukeReserve) >= preStop+prodStopWait.Max)
}

// The call's one deadline starts before it queues on the shared mutation
// lock: a slow lock holder, or slow work before the stop, refuses "host busy;
// retry" with nothing stopped, flipped or deleted.
func TestResidentDelete_HostBusyBeforeStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		// holdLock: another resident mutation holds the lock while the clock
		// passes the budget; slowClassify: classification eats it instead.
		holdLock     bool
		slowClassify bool
	}{
		{"slow lock holder", true, false},
		{"slow classification", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := useFakeResidentClock(t)
			s, d, task, role, runner, caller := busyDeleteFixture(t, true)
			// Past the point where Max + re-check + local nuke still fit.
			over := residentDeleteDeadline - residentPostStopReserve() + time.Second
			classified := false
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				classified = true
				if tc.slowClassify {
					clk.Advance(over)
				}
				return mergesafety.Verdict{Safe: true}, nil
			}
			if tc.holdLock {
				residentWorkerMutationMu.Lock()
			}
			done := make(chan struct{})
			var text string
			var isErr bool
			go func() {
				defer close(done)
				text, isErr = postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
			}()
			if tc.holdLock {
				// The handler has read its start time; now it queues.
				for clk.nowCalls() == 0 {
					time.Sleep(time.Millisecond)
				}
				clk.Advance(over)
				residentWorkerMutationMu.Unlock()
			}
			<-done
			testutil.True(t, isErr)
			testutil.Contains(t, text, "host busy; retry")
			testutil.Equal(t, classified, tc.slowClassify)
			testutil.Equal(t, runner.stopCount(), 0)
			testutil.True(t, runner.HasSession(task.ID))
			assertWorkerKept(t, d, task, role)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			// Not even the pre-stop in_review flip happened.
			testutil.Equal(t, got.Status, model.StatusInProgress)
		})
	}
}
