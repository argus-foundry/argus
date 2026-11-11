package bouncerecovery

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/transcriptstate"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func addTask(t *testing.T, d *db.DB, id, name string, status model.Status) {
	t.Helper()
	tk := &model.Task{ID: id, Name: name, Project: "p", Worktree: "/wt/" + id, Status: status}
	testutil.NoError(t, d.Add(tk))
}

func mkRole(t *testing.T, d *db.DB, orch int64, name string, kind db.HeraRoleKind) *db.HeraRole {
	t.Helper()
	r, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: orch, Name: name, Kind: kind, ArgusProject: "p", Prompt: "x"})
	testutil.NoError(t, err)
	return r
}

func bind(t *testing.T, d *db.DB, roleID int64, task string) {
	t.Helper()
	_, err := d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: roleID, ArgusTaskID: task, WorktreePath: "/wt/" + task})
	testutil.NoError(t, err)
}

// team builds orchestrator "o" with coordinator task "c" and workers "w1","w2".
func team(t *testing.T, d *db.DB) *db.HeraOrchestrator {
	t.Helper()
	o, err := d.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	for _, id := range []string{"c", "w1", "w2"} {
		addTask(t, d, id, "task-"+id, model.StatusInReview)
	}
	bind(t, d, mkRole(t, d, o.ID, "coord", db.HeraKindCoordinator).ID, "c")
	bind(t, d, mkRole(t, d, o.ID, "worker1", db.HeraKindWorker).ID, "w1")
	bind(t, d, mkRole(t, d, o.ID, "worker2", db.HeraKindWorker).ID, "w2")
	return o
}

func stateOf(m map[string]transcriptstate.State) classifyFn {
	return func(t *model.Task, _ *db.DB) transcriptstate.State {
		if s, ok := m[t.ID]; ok {
			return s
		}
		return transcriptstate.State{Kind: transcriptstate.Unknown}
	}
}

func TestRecord_PolicyMatrix(t *testing.T) {
	d := testDB(t)
	team(t, d)
	addTask(t, d, "plain-idle", "pi", model.StatusInReview)
	addTask(t, d, "plain-mid", "pm", model.StatusInReview)
	addTask(t, d, "arch", "ar", model.StatusInReview)
	testutil.NoError(t, d.SetArchived("arch", true))

	now := time.Unix(1_000_000, 0)
	n := record(d, []string{"c", "w1", "w2", "plain-idle", "plain-mid", "arch", "ghost"}, now, stateOf(map[string]transcriptstate.State{
		"c":          {Kind: transcriptstate.ParkedQuestion, Question: "Q?", Options: []string{"a", "b"}},
		"w1":         {Kind: transcriptstate.MidTurn},
		"w2":         {Kind: transcriptstate.Idle},
		"plain-idle": {Kind: transcriptstate.Idle},
		"plain-mid":  {Kind: transcriptstate.MidTurn},
	}))
	testutil.Equal(t, n, 5)

	got, err := LoadAll(d)
	testutil.NoError(t, err)
	testutil.Equal(t, got["c"].Status, StatusPending)
	testutil.Equal(t, got["c"].Role, RoleCoordinator)
	testutil.Equal(t, got["c"].Question, "Q?")
	testutil.DeepEqual(t, got["c"].Options, []string{"a", "b"})
	testutil.Equal(t, got["w1"].Status, StatusDelegated)
	testutil.Equal(t, got["w1"].CoordTask, "c")
	testutil.Equal(t, got["w2"].Status, StatusDelegated) // idle workers are still listed to their coordinator
	testutil.Equal(t, got["plain-idle"].Status, StatusDormant)
	testutil.Equal(t, got["plain-mid"].Status, StatusPending)
	testutil.Equal(t, got["plain-mid"].Role, RolePlain)
	_, has := got["arch"]
	testutil.Equal(t, has, false)
	testutil.Equal(t, got["c"].KilledAt.Unix(), now.Unix())
}

func TestRecord_WorkerWithoutKilledCoordinatorIsUnsupervised(t *testing.T) {
	d := testDB(t)
	team(t, d)
	record(d, []string{"w1", "w2"}, time.Unix(5, 0), stateOf(map[string]transcriptstate.State{
		"w1": {Kind: transcriptstate.MidTurn}, "w2": {Kind: transcriptstate.Idle},
	}))
	got, _ := LoadAll(d)
	testutil.Equal(t, got["w1"].Status, StatusPending)
	testutil.Equal(t, got["w2"].Status, StatusDormant)
}

func TestRecord_SubCoordinatorDepthAndParent(t *testing.T) {
	d := testDB(t)
	parent := team(t, d)
	addTask(t, d, "sub", "sub", model.StatusInReview)
	child, err := d.CreateHeraOrchestrator("child", "")
	testutil.NoError(t, err)
	bind(t, d, mkRole(t, d, child.ID, "coord", db.HeraKindCoordinator).ID, "sub")
	bridge := mkRole(t, d, parent.ID, "bridge", db.HeraKindWorker)
	bind(t, d, bridge.ID, "sub")
	testutil.NoError(t, d.CreateHeraOrchLink(parent.ID, child.ID, bridge.ID))

	record(d, []string{"c", "sub"}, time.Unix(5, 0), stateOf(nil))
	got, _ := LoadAll(d)
	testutil.Equal(t, got["sub"].Role, RoleCoordinator)
	testutil.Equal(t, got["sub"].Depth, 1)
	testutil.Equal(t, got["sub"].CoordTask, "c")
	testutil.Equal(t, got["sub"].Status, StatusPending) // coordinators always resume
	testutil.Equal(t, got["c"].Depth, 0)
}

func TestRecord_RewritesOnNewBounce(t *testing.T) {
	d := testDB(t)
	addTask(t, d, "p", "p", model.StatusInReview)
	record(d, []string{"p"}, time.Unix(100, 0), stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	got, _ := LoadAll(d)
	got["p"].Attempts = 2
	testutil.NoError(t, SetStatus(d, got["p"], StatusFailed))
	record(d, []string{"p"}, time.Unix(200, 0), stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	got, _ = LoadAll(d)
	testutil.Equal(t, got["p"].Status, StatusPending)
	testutil.Equal(t, got["p"].Attempts, 0)
	testutil.Equal(t, got["p"].BounceID, "200")
}

func TestLoadAll_IgnoresForeignRows(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.SetMeta("x", Namespace, "status", "pending")) // no killed_at
	got, err := LoadAll(d)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)
}

func TestBuildNote(t *testing.T) {
	killed := time.Date(2026, 11, 10, 12, 35, 0, 0, time.Local)
	t.Run("parked question", func(t *testing.T) {
		n := BuildNote(&Entry{State: transcriptstate.ParkedQuestion, KilledAt: killed, Question: "Where?\nline2", Options: []string{"A", "B"}, Role: RolePlain}, nil)
		testutil.True(t, strings.HasPrefix(n, "[argus] "))
		testutil.Contains(t, n, "> Where?\n> line2")
		testutil.Contains(t, n, "Options offered: A; B")
		testutil.Contains(t, n, "AskUserQuestion")
		testutil.Contains(t, n, "12:35")
	})
	t.Run("mid turn", func(t *testing.T) {
		n := BuildNote(&Entry{State: transcriptstate.MidTurn, KilledAt: killed, Role: RoleWorker}, nil)
		testutil.Contains(t, n, "mid-turn")
		testutil.Contains(t, n, "git status")
		testutil.True(t, !strings.Contains(n, "hera_revive"))
	})
	t.Run("coordinator team", func(t *testing.T) {
		team := []*Entry{
			{TaskID: "w1", RoleName: "worker1", Role: RoleWorker, State: transcriptstate.MidTurn},
			{TaskID: "s", RoleName: "sub", Role: RoleCoordinator, State: transcriptstate.ParkedQuestion},
			{TaskID: "w2", RoleName: "worker2", Role: RoleWorker, State: transcriptstate.Idle, Status: StatusDelegated},
		}
		n := BuildNote(&Entry{State: transcriptstate.MidTurn, KilledAt: killed, Role: RoleCoordinator}, team)
		testutil.Contains(t, n, "- worker1 (worker, was mid-turn)")
		testutil.Contains(t, n, "- sub (sub-coordinator, parked on a question to the user; argus is restarting it automatically)")
		testutil.Contains(t, n, "- worker2 (worker, idle; was idle between turns)")
		testutil.Contains(t, n, "hera_revive")
		testutil.Contains(t, n, "at most 3")
		testutil.Contains(t, n, "hera_board_post")
	})
	t.Run("coordinator no team", func(t *testing.T) {
		n := BuildNote(&Entry{State: transcriptstate.Idle, KilledAt: killed, Role: RoleCoordinator}, nil)
		testutil.Contains(t, n, "None of your workers")
	})
	t.Run("missing question text", func(t *testing.T) {
		n := BuildNote(&Entry{State: transcriptstate.ParkedQuestion, KilledAt: killed}, nil)
		testutil.Contains(t, n, "question text unavailable")
	})
}

type fakeRunner struct {
	alive   map[string]bool
	settled map[string]bool
	failFor map[string]int // remaining failures
	started []string
}

func (f *fakeRunner) IsAlive(id string) bool { return f.alive[id] }
func (f *fakeRunner) Settled(id string) bool { return f.settled[id] }
func (f *fakeRunner) Resume(id string) error {
	if f.failFor[id] > 0 {
		f.failFor[id]--
		return errors.New("boom")
	}
	f.started = append(f.started, id)
	return nil
}

type fakeNotifier struct{ sent map[string]string }

func (f *fakeNotifier) ReliableNotify(id, text, _ string, _ notify.NotifyOpts) func() {
	f.sent[id] = text
	return func() {}
}

type harness struct {
	d   *db.DB
	r   *fakeRunner
	n   *fakeNotifier
	w   *Worker
	now time.Time
	cfg config.Config
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{d: testDB(t), r: &fakeRunner{alive: map[string]bool{}, settled: map[string]bool{}, failFor: map[string]int{}}, n: &fakeNotifier{sent: map[string]string{}}, now: time.Unix(1_000_000, 0), cfg: config.DefaultConfig()}
	h.w = NewWorker(h.d, h.r, h.n, func() config.Config { return h.cfg })
	h.w.now = func() time.Time { return h.now }
	return h
}

func (h *harness) tick(advance time.Duration) { h.now = h.now.Add(advance); h.w.Tick() }

func TestWorker_CoordinatorsFirstThenThrottle(t *testing.T) {
	h := newHarness(t)
	team(t, h.d)
	addTask(t, h.d, "p1", "p1", model.StatusInReview)
	addTask(t, h.d, "p2", "p2", model.StatusInReview)
	addTask(t, h.d, "p3", "p3", model.StatusInReview)
	addTask(t, h.d, "p4", "p4", model.StatusInReview)
	mid := transcriptstate.State{Kind: transcriptstate.MidTurn}
	record(h.d, []string{"p1", "p2", "p3", "p4", "w1", "c"}, h.now, stateOf(map[string]transcriptstate.State{"p1": mid, "p2": mid, "p3": mid, "p4": mid, "w1": mid, "c": mid}))
	h.cfg.BounceRecovery.MaxConcurrent = 3

	h.w.Tick()
	testutil.DeepEqual(t, h.r.started, []string{"c"}) // coordinator first
	testutil.Contains(t, h.n.sent["c"], "worker1")    // team lists the delegated worker
	h.w.Tick()                                        // inside stagger window: nothing
	testutil.Equal(t, len(h.r.started), 1)
	h.tick(5 * time.Second)
	h.tick(5 * time.Second)
	testutil.Equal(t, len(h.r.started), 3) // cap of 3 unsettled reached
	h.tick(5 * time.Second)
	testutil.Equal(t, len(h.r.started), 3)
	h.r.settled["p1"] = true // one settles → room for one more
	h.tick(5 * time.Second)
	testutil.Equal(t, len(h.r.started), 4)
	h.tick(61 * time.Second) // settle ceiling frees the rest
	h.tick(5 * time.Second)
	testutil.Equal(t, len(h.r.started), 5)
	for _, id := range h.r.started {
		testutil.True(t, id != "w1") // delegated worker never started by the daemon
	}
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["w1"].Status, StatusDelegated)
	testutil.Equal(t, got["c"].Status, StatusResumed)
}

func TestWorker_DisabledDoesNothing(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	h.cfg.BounceRecovery.Enabled = false
	h.w.Tick()
	testutil.Equal(t, len(h.r.started), 0)
}

func TestWorker_ExpiryNeverResumes(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	h.tick(7 * time.Hour)
	testutil.Equal(t, len(h.r.started), 0)
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["p"].Status, StatusExpired)
}

func TestWorker_SupersededWhenAliveOrInProgress(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "alive", "a", model.StatusInReview)
	addTask(t, h.d, "human", "h", model.StatusInProgress)
	addTask(t, h.d, "done", "d", model.StatusComplete)
	mid := transcriptstate.State{Kind: transcriptstate.MidTurn}
	record(h.d, []string{"alive", "human", "done"}, h.now, stateOf(map[string]transcriptstate.State{"alive": mid, "human": mid, "done": mid}))
	h.r.alive["alive"] = true
	for i := 0; i < 3; i++ {
		h.tick(5 * time.Second)
	}
	testutil.Equal(t, len(h.r.started), 0)
	got, _ := LoadAll(h.d)
	for _, id := range []string{"alive", "human", "done"} {
		testutil.Equal(t, got[id].Status, StatusSuperseded)
	}
}

func TestWorker_RetryBackoffThenFail(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	h.r.failFor["p"] = 5
	h.w.Tick()
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["p"].Status, StatusPending)
	testutil.Equal(t, got["p"].Attempts, 1)
	h.tick(5 * time.Second) // before 10s backoff: no attempt
	got, _ = LoadAll(h.d)
	testutil.Equal(t, got["p"].Attempts, 1)
	h.tick(6 * time.Second)
	h.tick(31 * time.Second)
	got, _ = LoadAll(h.d)
	testutil.Equal(t, got["p"].Status, StatusFailed)
	testutil.Equal(t, got["p"].Attempts, MaxAttempts)
}

func TestWorker_DelegatedPromotedWhenCoordinatorFails(t *testing.T) {
	h := newHarness(t)
	team(t, h.d)
	mid := transcriptstate.State{Kind: transcriptstate.MidTurn}
	record(h.d, []string{"c", "w1", "w2"}, h.now, stateOf(map[string]transcriptstate.State{"c": mid, "w1": mid, "w2": {Kind: transcriptstate.Idle}}))
	h.d.Update(&model.Task{ID: "c", Name: "task-c", Project: "p", Worktree: "/wt/c", Status: model.StatusInReview, Archived: true})
	h.r.failFor["c"] = 9
	for i := 0; i < 6; i++ {
		h.tick(40 * time.Second)
	}
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["c"].Status, StatusSuperseded) // archived coordinator
	// workers re-evaluated as unsupervised: mid resumes, idle goes dormant
	testutil.Equal(t, got["w2"].Status, StatusDormant)
	testutil.DeepEqual(t, h.r.started, []string{"w1"})
}

func TestWorker_WaitsForReady(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	ready := false
	h.w.Ready = func() bool { return ready }
	h.w.Tick()
	testutil.Equal(t, len(h.r.started), 0)
	ready = true
	h.w.Tick()
	testutil.Equal(t, len(h.r.started), 1)
}
