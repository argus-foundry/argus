package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// seedRecyclePlainTask creates a plain (non-hera) task for taskRecycleRunner
// tests, mirroring seedHeraRecycleCoordinator (recycle_test.go) but with no
// orchestrator/role — task_recycle works on any argus task.
func seedRecyclePlainTask(t *testing.T, database *db.DB, worktree, prompt string) *model.Task {
	t.Helper()
	task := &model.Task{
		ID:       "plain-task",
		Name:     "plain-task",
		Status:   model.StatusInProgress,
		Project:  "test-project",
		Worktree: worktree,
		Backend:  "test",
		Prompt:   prompt,
	}
	testutil.NoError(t, database.Add(task))
	return task
}

func TestBuildTaskRecycleSeedPrompt(t *testing.T) {
	got := buildTaskRecycleSeedPrompt("original mission text", "did X, next do Y", "")

	testutil.Contains(t, got, "did X, next do Y")
	testutil.Contains(t, got, "original mission text")

	// The handoff note must appear before the original prompt (design.md D5's
	// ordering rationale, mirrored from hera.BuildRecycleSeedPrompt: a fresh
	// session anchors on current state, not a stale "start from scratch"
	// instruction).
	if strings.Index(got, "did X, next do Y") > strings.Index(got, "original mission text") {
		t.Fatal("handoff note must appear before the original prompt")
	}
}

func TestResidentRecycleSeedOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, memoryDir, prefix string }{
		{"default", "", "resident"},
		{"configured", "custom/memory", "custom/memory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exchange := "\n## Last exchange with the user before the reset\nQ\n"
			got := buildResidentRecycleSeedPrompt("persistent orientation", "handoff first", tc.memoryDir, exchange)
			parts := []string{"handoff first", "## Last exchange with the user before the reset", "Open with the user — relay first", tc.prefix + "/open-loops.md", tc.prefix + "/profile.md", "then continue", "persistent orientation"}
			previous := -1
			for _, part := range parts {
				idx := strings.Index(got, part)
				testutil.Equal(t, idx > previous, true)
				previous = idx
			}
			testutil.Contains(t, got, "background only")

			// The relay directive is unconditional, even with no exchange.
			empty := buildResidentRecycleSeedPrompt("persistent orientation", "handoff first", tc.memoryDir, "")
			testutil.Contains(t, empty, "Open with the user — relay first")
			testutil.False(t, strings.Contains(empty, "## Last exchange with the user before the reset"))
		})
	}
}

// residentRecycleCaptureRunner records how a recycle restarts the resident.
// Get reports no session on its first call (so awaitIdleAndRestart proceeds at
// once) and, when live is set, a live session afterwards so the restart goes
// through Recycle rather than Start.
type residentRecycleCaptureRunner struct {
	agent.SessionRunner
	live        bool
	gets        int
	started     *model.Task
	startResume bool
	recycled    *model.Task
}

type residentRecycleHandle struct{ agent.SessionHandle }

func (residentRecycleHandle) PTYSize() (int, int) { return 24, 80 }

func (r *residentRecycleCaptureRunner) Get(string) agent.SessionHandle {
	r.gets++
	if r.live && r.gets > 1 {
		return residentRecycleHandle{}
	}
	return nil
}

func (r *residentRecycleCaptureRunner) Start(task *model.Task, _ config.Config, _, _ uint16, resume bool) (agent.SessionHandle, error) {
	copyTask := *task
	r.started = &copyTask
	r.startResume = resume
	return nil, nil
}

func (r *residentRecycleCaptureRunner) Recycle(task *model.Task, _ config.Config, _, _ uint16) error {
	copyTask := *task
	r.recycled = &copyTask
	return nil
}

// TestResidentRecycleUsesPersistentOrientation pins the resident recycle after
// fix-recycle-session-id: the restart mints a fresh pinned session ID (never
// the old one, never a --resume) and launches the resident seed — handoff,
// then open loops, then the persistent orientation — whether or not a session
// is still live when the restart runs.
func TestResidentRecycleUsesPersistentOrientation(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "no live session", true: "live session"}[live], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			database, err := db.OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = database.Close() })
			task, _, err := database.EnsureResidentTask(t.TempDir())
			testutil.NoError(t, err)
			task.Prompt = "obsolete prior recycle seed"
			task.Backend = "test"
			task.SessionID = "old-session"
			testutil.NoError(t, database.Update(task))
			originalOrientation := residentOrientationPrompt
			residentOrientationPrompt = func(config.Config) string { return "persistent orientation" }
			t.Cleanup(func() { residentOrientationPrompt = originalOrientation })
			runner := &residentRecycleCaptureRunner{live: live}
			cfg := recycleTestConfig()
			cfg.Resident.MemoryDir = "custom/memory"
			r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
			r.spawn = func(f func()) { f() }
			testutil.NoError(t, r.Recycle(task.ID, "current handoff"))

			launched := runner.started
			if live {
				testutil.Nil(t, runner.started)
				launched = runner.recycled
			} else {
				testutil.Nil(t, runner.recycled)
				testutil.False(t, runner.startResume)
			}
			if launched == nil {
				t.Fatal("recycle never relaunched the resident")
			}
			testutil.True(t, launched.IsResident())
			testutil.True(t, launched.SessionID != "" && launched.SessionID != "old-session")
			p := launched.Prompt
			handoff := strings.Index(p, "current handoff")
			loops := strings.Index(p, "custom/memory/open-loops.md")
			orient := strings.Index(p, "persistent orientation")
			relay := strings.Index(p, "Open with the user — relay first")
			testutil.True(t, handoff >= 0 && relay > handoff && loops > relay && orient > loops)
			testutil.False(t, strings.Contains(p, "obsolete prior recycle seed"))

			// The persisted row matches what was launched, so a later crash
			// restart resumes the recycled session, not the old one.
			stored, err := database.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.SessionID, launched.SessionID)
			testutil.Equal(t, stored.Prompt, p)
		})
	}
}

func TestTaskRecycleRunner_Restart_ClearsSessionAndSeedsPrompt(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	task := seedRecyclePlainTask(t, database, t.TempDir(), "original mission")
	task.SessionID = "stale-sid-1"
	testutil.NoError(t, database.Update(task))

	runner := agent.NewRunner(nil)
	cfg := recycleTestConfig()
	sess1, err := runner.Start(task, cfg, 24, 80, false)
	testutil.NoError(t, err)
	t.Cleanup(runner.StopAll)

	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
	testutil.NoError(t, r.restart(task.ID, func(*model.Task) string { return "the seed prompt" }))

	updated, err := database.Get(task.ID)
	testutil.NoError(t, err)
	// Recycle mints a fresh pinned ID (not "", not the stale one): an empty
	// ID leaves the live session un-resumable (ctrl+n re-render refuses).
	testutil.Equal(t, updated.SessionID != "" && updated.SessionID != "stale-sid-1", true)
	testutil.Equal(t, updated.Prompt, "the seed prompt")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runner.HasPendingRestart(task.ID) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		newSess := runner.Get(task.ID)
		if newSess != nil && newSess != sess1 && newSess.Alive() {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for restart to resurrect the session")
}

func TestTaskRecycleRunner_Restart_NoLiveSession_StartsFresh(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	task := seedRecyclePlainTask(t, database, t.TempDir(), "original mission")

	runner := agent.NewRunner(nil) // no session ever started for this task
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })

	testutil.NoError(t, r.restart(task.ID, func(*model.Task) string { return "the seed prompt" }))
	t.Cleanup(runner.StopAll)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sess := runner.Get(task.ID); sess != nil && sess.Alive() {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for restart to start a fresh session when none existed")
}

func TestTaskRecycleRunner_Restart_UnknownTaskErrors(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	runner := agent.NewRunner(nil)
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })

	if err := r.restart("no-such-task", func(*model.Task) string { return "seed" }); err == nil {
		t.Fatal("expected an error for an unknown task")
	}
}

// TestTaskRecycleRunner_AwaitIdleAndRestart_NoSessionRestartsImmediately pins
// awaitIdleAndRestart's "missing session counts as idle" behavior (mirrors
// HeraRecycleRunner.IsIdle's same rule): a task with no live session — e.g.
// one that already exited before this goroutine got scheduled — must restart
// on the very first check, not hang waiting for a session that will never
// exist.
func TestTaskRecycleRunner_AwaitIdleAndRestart_NoSessionRestartsImmediately(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	task := seedRecyclePlainTask(t, database, t.TempDir(), "original mission")

	runner := agent.NewRunner(nil) // Get(task.ID) returns nil
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
	r.sleep = func(time.Duration) {}

	r.awaitIdleAndRestart(task.ID, func(*model.Task) string { return "the seed prompt" })
	t.Cleanup(runner.StopAll)

	updated, err := database.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, updated.Prompt, "the seed prompt")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sess := runner.Get(task.ID); sess != nil && sess.Alive() {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for a fresh session to start")
}

// TestTaskRecycleRunner_AwaitIdleAndRestart_NeverIdleGivesUp pins the timeout
// guard: a session that never goes idle (recycleTestConfig's backend emits no
// output at all, so Session.IsIdle's "still starting up" branch never
// clears) must not restart the task, and the poll loop must not hang
// forever.
func TestTaskRecycleRunner_AwaitIdleAndRestart_NeverIdleGivesUp(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	task := seedRecyclePlainTask(t, database, t.TempDir(), "original mission")
	task.SessionID = "stale-sid-1"
	testutil.NoError(t, database.Update(task))

	runner := agent.NewRunner(nil)
	cfg := recycleTestConfig()
	_, err = runner.Start(task, cfg, 24, 80, false)
	testutil.NoError(t, err)
	t.Cleanup(runner.StopAll)

	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
	r.sleep = func(time.Duration) {}
	r.idleTimeout = 0 // expire on the very first deadline check

	r.awaitIdleAndRestart(task.ID, func(*model.Task) string { return "the seed prompt" })

	// Restart must NOT have happened: SessionID/Prompt unchanged.
	updated, err := database.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, updated.SessionID, "stale-sid-1")
	testutil.Equal(t, updated.Prompt, "original mission")

	// The seed prompt must be persisted to task_meta instead of silently
	// discarded — the MCP call already told the caller a fresh session
	// would start "shortly."
	meta, err := database.ListMeta(task.ID, taskRecycleMetaNamespace)
	testutil.NoError(t, err)
	found := false
	for _, e := range meta {
		if e.Key == taskRecycleMetaKeyTimedOutPrompt {
			testutil.Equal(t, e.Value, "the seed prompt")
			found = true
		}
	}
	if !found {
		t.Fatal("expected the timed-out seed prompt to be persisted to task_meta")
	}
}

func TestTaskRecycleRunner_Recycle_SchedulesRestart(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	task := seedRecyclePlainTask(t, database, t.TempDir(), "original mission")

	runner := agent.NewRunner(nil) // no session ever started -> idle immediately
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
	r.sleep = func(time.Duration) {}
	// Run synchronously so the assertions below don't race a background goroutine.
	r.spawn = func(f func()) { f() }

	testutil.NoError(t, r.Recycle(task.ID, "did X, next do Y"))
	t.Cleanup(runner.StopAll)

	updated, err := database.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Contains(t, updated.Prompt, "did X, next do Y")
	testutil.Contains(t, updated.Prompt, "original mission")
}

func TestTaskRecycleRunner_Recycle_UnknownTaskErrors(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	runner := agent.NewRunner(nil)
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })

	if err := r.Recycle("no-such-task", "note"); err == nil {
		t.Fatal("expected an error for an unknown task")
	}
}

// TestTaskRecycleRunner_Recycle_UnknownTaskReleasesInFlight pins that a
// failed Recycle (task lookup error) releases its inFlight reservation —
// otherwise an unknown-task typo would permanently wedge that task ID
// against ever recycling again, even once a task with that ID exists.
func TestTaskRecycleRunner_Recycle_UnknownTaskReleasesInFlight(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	runner := agent.NewRunner(nil)
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })

	if err := r.Recycle("no-such-task", "note"); err == nil {
		t.Fatal("expected an error for an unknown task")
	}
	if _, stillHeld := r.inFlight.Load("no-such-task"); stillHeld {
		t.Fatal("a failed Recycle must release its inFlight reservation")
	}
}

// TestTaskRecycleRunner_Recycle_RejectsConcurrentCallsForSameTask pins the
// dedup guard: a second Recycle call for a task that already has one
// in-flight is rejected outright, rather than racing a second
// awaitIdleAndRestart poller against the first (see inFlight's doc comment
// for the stale-seed-prompt race this prevents).
func TestTaskRecycleRunner_Recycle_RejectsConcurrentCallsForSameTask(t *testing.T) {
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	task := seedRecyclePlainTask(t, database, t.TempDir(), "original mission")

	runner := agent.NewRunner(nil)
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
	// Never actually run the poll loop, so the first call's reservation
	// stays held for the second call to collide with.
	r.spawn = func(func()) {}

	testutil.NoError(t, r.Recycle(task.ID, "first handoff"))

	err = r.Recycle(task.ID, "second handoff")
	if err == nil {
		t.Fatal("expected the second concurrent Recycle call to be rejected")
	}
	testutil.Contains(t, err.Error(), "already in progress")

	// The task row must be untouched by the rejected second call.
	unchanged, err := database.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, unchanged.Prompt, "original mission")
}
