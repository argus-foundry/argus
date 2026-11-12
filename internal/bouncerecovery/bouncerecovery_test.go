package bouncerecovery

import (
	"errors"
	"strconv"
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
	return func(t *model.Task, _ *db.DB) (transcriptstate.State, bool) {
		if s, ok := m[t.ID]; ok {
			return s, false
		}
		return transcriptstate.State{Kind: transcriptstate.Unknown}, false
	}
}

// stateOfNoTranscript is stateOf where the listed tasks have no transcript.
func stateOfNoTranscript(m map[string]transcriptstate.State, missing ...string) classifyFn {
	base := stateOf(m)
	gone := map[string]bool{}
	for _, id := range missing {
		gone[id] = true
	}
	return func(t *model.Task, d *db.DB) (transcriptstate.State, bool) {
		st, _ := base(t, d)
		return st, gone[t.ID]
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
	n := record(d, []string{"c", "w1", "w2", "plain-idle", "plain-mid", "arch", "ghost"}, nil, now, stateOf(map[string]transcriptstate.State{
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
	record(d, []string{"w1", "w2"}, nil, time.Unix(5, 0), stateOf(map[string]transcriptstate.State{
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

	record(d, []string{"c", "sub"}, nil, time.Unix(5, 0), stateOf(nil))
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
	record(d, []string{"p"}, nil, time.Unix(100, 0), stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	got, _ := LoadAll(d)
	got["p"].Attempts = 2
	testutil.NoError(t, SetStatus(d, got["p"], StatusFailed))
	record(d, []string{"p"}, nil, time.Unix(200, 0), stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
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
		testutil.Contains(t, n, `Options offered (also data): "A"; "B"`)
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
		testutil.Contains(t, n, "recorded no killed workers")
		testutil.True(t, !strings.Contains(n, "None of your"))
		testutil.Contains(t, n, "incomplete")
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

type fakeNotifier struct {
	sent      map[string]string
	ids       map[string]string // task → last delivery ID
	calls     map[string]int
	submitted map[string]bool // delivery ID → went out
}

func (f *fakeNotifier) ReliableNotify(id, text, did string, _ notify.NotifyOpts) func() {
	f.sent[id] = text
	f.ids[id] = did
	f.calls[id]++
	return func() {}
}

func (f *fakeNotifier) Submitted(_, did string) bool { return f.submitted[did] }

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
	h := &harness{d: testDB(t), r: &fakeRunner{alive: map[string]bool{}, settled: map[string]bool{}, failFor: map[string]int{}}, n: &fakeNotifier{sent: map[string]string{}, ids: map[string]string{}, calls: map[string]int{}, submitted: map[string]bool{}}, now: time.Unix(1_000_000, 0), cfg: config.DefaultConfig()}
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
	record(h.d, []string{"p1", "p2", "p3", "p4", "w1", "c"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p1": mid, "p2": mid, "p3": mid, "p4": mid, "w1": mid, "c": mid}))
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
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
	h.cfg.BounceRecovery.Enabled = false
	h.w.Tick()
	testutil.Equal(t, len(h.r.started), 0)
}

func TestWorker_ExpiryNeverResumes(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
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
	record(h.d, []string{"alive", "human", "done"}, nil, h.now, stateOf(map[string]transcriptstate.State{"alive": mid, "human": mid, "done": mid}))
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
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.MidTurn}}))
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

func midState() transcriptstate.State { return transcriptstate.State{Kind: transcriptstate.MidTurn} }

func TestWorker_DelegatedPromotedWhenCoordinatorFails(t *testing.T) {
	h := newHarness(t)
	team(t, h.d)
	record(h.d, []string{"c", "w1", "w2"}, nil, h.now, stateOf(map[string]transcriptstate.State{"c": midState(), "w1": midState(), "w2": {Kind: transcriptstate.Idle}}))
	h.r.failFor["c"] = 9 // the coordinator's resume keeps failing: it really reaches StatusFailed
	for i := 0; i < 8; i++ {
		h.tick(40 * time.Second)
	}
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["c"].Status, StatusFailed)
	testutil.Equal(t, got["c"].Attempts, MaxAttempts)
	testutil.Equal(t, got["w2"].Status, StatusDormant) // idle worker falls back to dormant
	testutil.DeepEqual(t, h.r.started, []string{"w1"}) // mid-turn worker resumed directly
}

func TestWorker_ArchivedCoordinatorSupersedesWorkers(t *testing.T) {
	h := newHarness(t)
	team(t, h.d)
	record(h.d, []string{"c", "w1", "w2"}, nil, h.now, stateOf(map[string]transcriptstate.State{"c": midState(), "w1": midState(), "w2": midState()}))
	// The user abandons the orchestration after the bounce but before recovery runs.
	testutil.NoError(t, h.d.SetArchived("c", true))
	h.tick(5 * time.Second)
	got, _ := LoadAll(h.d)
	// SetArchived wipes the coordinator's own ledger entry with its task_meta;
	// its workers must be superseded, never promoted.
	testutil.Equal(t, got["w1"].Status, StatusSuperseded)
	testutil.Equal(t, got["w1"].Reason, "coordinator_closed")
	testutil.Equal(t, got["w2"].Status, StatusSuperseded)
	testutil.Equal(t, len(h.r.started), 0)
}

func TestRecord_ArchivedCoordinatorSupersedesWorkersAtRecordTime(t *testing.T) {
	d := testDB(t)
	team(t, d)
	testutil.NoError(t, d.SetArchived("c", true))
	record(d, []string{"w1"}, nil, time.Unix(5, 0), stateOf(map[string]transcriptstate.State{"w1": midState()}))
	got, _ := LoadAll(d)
	testutil.Equal(t, got["w1"].Status, StatusSuperseded)
	testutil.Equal(t, got["w1"].Reason, "coordinator_closed")
}

func TestRecord_CompletedCoordinatorSupersedesWorkers(t *testing.T) {
	d := testDB(t)
	team(t, d)
	tk, _ := d.Get("c")
	tk.Status = model.StatusComplete
	testutil.NoError(t, d.Update(tk))
	record(d, []string{"w1"}, nil, time.Unix(5, 0), stateOf(map[string]transcriptstate.State{"w1": midState()}))
	got, _ := LoadAll(d)
	testutil.Equal(t, got["w1"].Status, StatusSuperseded)
}

func TestRecord_NoTranscriptIsNeverResumed(t *testing.T) {
	d := testDB(t)
	addTask(t, d, "p", "p", model.StatusInReview)
	record(d, []string{"p"}, nil, time.Unix(5, 0), stateOfNoTranscript(nil, "p"))
	got, _ := LoadAll(d)
	testutil.Equal(t, got["p"].Status, StatusFailed)
	testutil.Equal(t, got["p"].Reason, "no_transcript")
}

func TestWorker_CoordinatorWithoutTranscriptFallsBackToResumingWorkers(t *testing.T) {
	h := newHarness(t)
	team(t, h.d)
	record(h.d, []string{"c", "w1", "w2"}, nil, h.now, stateOfNoTranscript(map[string]transcriptstate.State{"c": midState(), "w1": midState(), "w2": {Kind: transcriptstate.Idle}}, "c"))
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["c"].Status, StatusFailed)
	testutil.Equal(t, got["w1"].Status, StatusDelegated) // delegated at record time...
	h.w.Tick()                                           // ...and promoted on the first tick
	testutil.DeepEqual(t, h.r.started, []string{"w1"})   // worker resumed directly; coordinator never started
	got, _ = LoadAll(h.d)
	testutil.Equal(t, got["w2"].Status, StatusDormant)
	testutil.Equal(t, h.n.sent["c"], "")
}

func TestRecord_IdleInReviewTeamMembersAreListedNotResumed(t *testing.T) {
	d := testDB(t)
	team(t, d) // c, w1, w2 all in_review
	addTask(t, d, "w3", "task-w3", model.StatusInReview)
	o, _ := d.HeraOrchestratorByName("o")
	bind(t, d, mkRole(t, d, o.ID, "worker3", db.HeraKindWorker).ID, "w3")
	// c and w1 were running (in_progress orphans); w2 and w3 were idle, rolled to in_review.
	for _, id := range []string{"c", "w1"} {
		tk, _ := d.Get(id)
		tk.Status = model.StatusInProgress
		testutil.NoError(t, d.Update(tk))
	}
	n := record(d, []string{"c", "w1"}, func(id string) bool { return id == "w3" }, time.Unix(5, 0), stateOf(map[string]transcriptstate.State{
		"c": midState(), "w1": midState(), "w2": {Kind: transcriptstate.Idle},
	}))
	testutil.Equal(t, n, 3) // c, w1 and the idle in_review w2; w3 is alive so it is not recorded
	got, _ := LoadAll(d)
	testutil.Equal(t, got["w2"].Status, StatusDormant)
	testutil.Equal(t, got["w2"].Reason, "in_review")
	testutil.Equal(t, got["w2"].CoordTask, "c")
	_, w3 := got["w3"]
	testutil.Equal(t, w3, false)
	for _, id := range []string{"c", "w1"} { // the reconcile flips orphans to in_review
		tk, _ := d.Get(id)
		tk.Status = model.StatusInReview
		testutil.NoError(t, d.Update(tk))
	}

	h := newHarness(t)
	h.d = d
	h.w = NewWorker(d, h.r, h.n, func() config.Config { return h.cfg })
	h.w.now = func() time.Time { return time.Unix(10, 0) }
	h.w.Tick()
	note := h.n.sent["c"]
	testutil.Contains(t, note, "worker2")
	testutil.Contains(t, note, "idle in review, not restarted")
	testutil.True(t, !strings.Contains(note, "None of your"))
	testutil.DeepEqual(t, h.r.started, []string{"c"}) // the idle member is never auto-resumed
}

func TestWorker_NotePersistedAndRequeuedAfterDaemonBounce(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": midState()}))
	h.w.Tick()
	did := h.n.ids["p"]
	testutil.Equal(t, did, "bounce-recovery:p:"+strconv.FormatInt(h.now.Unix(), 10))
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["p"].Status, StatusResumed)
	testutil.Equal(t, got["p"].NoteState, "") // resumed but NOT noted: delivery has not gone out

	// The daemon bounces inside the delivery window: a fresh worker has no
	// in-memory queue. It must re-queue under the SAME delivery ID.
	w2 := NewWorker(h.d, h.r, h.n, func() config.Config { return h.cfg })
	w2.now = func() time.Time { return h.now.Add(time.Minute) }
	w2.Tick()
	testutil.Equal(t, h.n.calls["p"], 2)
	testutil.Equal(t, h.n.ids["p"], did)

	// Once the notifier reports it submitted, the entry becomes noted and is never re-queued.
	h.n.submitted[did] = true
	w2.now = func() time.Time { return h.now.Add(2 * time.Minute) }
	w2.Tick()
	got, _ = LoadAll(h.d)
	testutil.Equal(t, got["p"].NoteState, NoteSent)
	w2.Tick()
	testutil.Equal(t, h.n.calls["p"], 2)
}

func TestWorker_NoteAbandonedAfterRepeatedUndelivery(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": midState()}))
	for i := 0; i < 5; i++ {
		h.tick(11 * time.Minute) // each step outlives the notifier's delivery deadline
	}
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["p"].NoteState, NoteAbandoned)
	testutil.Equal(t, h.n.calls["p"], maxNoteQueues)
}

func TestWorker_NoNoteOnFailedStart(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": midState()}))
	h.r.failFor["p"] = 9
	for i := 0; i < 6; i++ {
		h.tick(40 * time.Second)
	}
	testutil.Equal(t, h.n.calls["p"], 0)
}

func TestWorker_NoteCarriesQuotedQuestionAndOptions(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": {Kind: transcriptstate.ParkedQuestion, Question: "Which?", Options: []string{"a\nb", `say "hi"`}}}))
	h.w.Tick()
	n := h.n.sent["p"]
	testutil.Contains(t, n, "> Which?")
	testutil.Contains(t, n, `"a b"; "say 'hi'"`)
}

func TestWorker_MinSettleHoldsSlot(t *testing.T) {
	h := newHarness(t)
	h.cfg.BounceRecovery.MaxConcurrent = 1
	addTask(t, h.d, "p1", "p1", model.StatusInReview)
	addTask(t, h.d, "p2", "p2", model.StatusInReview)
	record(h.d, []string{"p1", "p2"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p1": midState(), "p2": midState()}))
	h.r.settled["p1"] = true // reads idle immediately, as a fresh session does
	h.w.Tick()
	h.tick(5 * time.Second) // stagger (4s) has passed but minSettle (8s) has not
	testutil.Equal(t, len(h.r.started), 1)
	h.tick(4 * time.Second) // now 9s after the start
	testutil.Equal(t, len(h.r.started), 2)
}

func TestWorker_ExpiryRunsBeforeReadyGateAndLogsOnce(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "p", "p", model.StatusInReview)
	record(h.d, []string{"p"}, nil, h.now, stateOf(map[string]transcriptstate.State{"p": midState()}))
	h.w.Ready = func() bool { return false }
	h.tick(7 * time.Hour)
	got, _ := LoadAll(h.d)
	testutil.Equal(t, got["p"].Status, StatusExpired) // expired even though the MCP port never came up
	testutil.True(t, h.w.loggedWaiting)
	testutil.Equal(t, len(h.r.started), 0)
}

func TestWorker_PrunesFinishedEntriesOlderThanExpiry(t *testing.T) {
	h := newHarness(t)
	addTask(t, h.d, "dorm", "d", model.StatusInReview)
	addTask(t, h.d, "act", "a", model.StatusInReview)
	record(h.d, []string{"dorm", "act"}, nil, h.now, stateOf(map[string]transcriptstate.State{"dorm": {Kind: transcriptstate.Idle}, "act": midState()}))
	h.tick(7 * time.Hour) // act expires; dorm (finished) is pruned
	got, _ := LoadAll(h.d)
	_, hasDorm := got["dorm"]
	testutil.Equal(t, hasDorm, false)
	testutil.Equal(t, got["act"].Status, StatusExpired)
	h.tick(time.Second) // expired entry is finished and also old: pruned next pass
	got, _ = LoadAll(h.d)
	testutil.Equal(t, len(got), 0)
}
