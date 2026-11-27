package hera

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type reapFixture struct {
	d       *db.DB
	orch    *db.HeraOrchestrator
	coord   *db.HeraRole
	runner  *fakeArchiveRunner
	clock   time.Time
	git     GitCheck
	handoff HandoffCheck
	checks  int // handoff evaluations
	mu      sync.Mutex
	sent    []*db.HeraMessage
}

func newReapFixture(t *testing.T) *reapFixture {
	t.Helper()
	d := archDB(t)
	orch, err := d.CreateHeraOrchestrator("o", "master")
	testutil.NoError(t, err)
	f := &reapFixture{d: d, orch: orch, runner: &fakeArchiveRunner{alive: true, idle: true}, clock: time.Unix(1_800_000_000, 0), git: gitBad}
	f.handoff = func(t *model.Task) (HandoffInfo, bool, string) { f.checks++; return handoffOK(t) }
	f.coord = reapCoord(t, d, orch.ID, "coord")
	return f
}

func reapCoord(t *testing.T, d *db.DB, orchID int64, name string) *db.HeraRole {
	t.Helper()
	task := &model.Task{Name: name, Status: model.StatusInProgress, Project: "p", Worktree: "/wt/" + name}
	testutil.NoError(t, d.Add(task))
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: orchID, Name: name, Kind: db.HeraKindCoordinator, ArgusProject: "p",
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return role
}

func (f *reapFixture) nudger() *ReapNudger {
	n := NewReapNudger(f.d, f.runner, func(t *model.Task) (bool, string) { return f.git(t) }, func(t *model.Task) (HandoffInfo, bool, string) { return f.handoff(t) }, func(m *db.HeraMessage) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sent = append(f.sent, m)
	})
	n.now = func() time.Time { return f.clock }
	return n
}

func (f *reapFixture) inbox(t *testing.T) []*db.HeraMessage {
	t.Helper()
	msgs, err := f.d.HeraInbox(f.coord.ID)
	testutil.NoError(t, err)
	return msgs
}

func TestReapNudge_BatchesHandedOffRolesIntoOneMessage(t *testing.T) {
	f := newReapFixture(t)
	ta, _ := archWorker(t, f.d, f.orch.ID, "fqs-pr2", model.StatusInReview)
	archWorker(t, f.d, f.orch.ID, "fqs-pr3", model.StatusInReview)
	f.nudger().Sweep()

	msgs := f.inbox(t)
	testutil.Equal(t, len(msgs), 1)
	testutil.Equal(t, len(f.sent), 1)
	m := msgs[0]
	testutil.Equal(t, strings.HasPrefix(m.Body, "[argus] 2 roles ready to archive: "), true)
	testutil.Contains(t, m.Body, "fqs-pr2 (handed off), fqs-pr3 (handed off)")
	testutil.Contains(t, m.Body, "hera_archive")
	testutil.Equal(t, m.Tldr, "2 roles ready to archive: fqs-pr2, fqs-pr3")
	testutil.Equal(t, m.SenderName("coord"), "argus")
	got, err := f.d.Get(ta.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Archived, false) // the daemon only suggests
	testutil.Equal(t, len(f.runner.stopped), 0)
}

func TestReapNudge_MergedKindLabel(t *testing.T) {
	f := newReapFixture(t)
	f.git = gitOK
	archWorker(t, f.d, f.orch.ID, "done", model.StatusComplete)
	f.nudger().Sweep()
	msgs := f.inbox(t)
	testutil.Equal(t, len(msgs), 1)
	testutil.Contains(t, msgs[0].Body, "[argus] 1 role ready to archive: done (merged)")
}

func TestReapNudge_RefusedAndExcludedRolesAreNotNudged(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, f *reapFixture)
	}{
		{"in progress", func(t *testing.T, f *reapFixture) {
			archWorker(t, f.d, f.orch.ID, "w", model.StatusInProgress)
		}},
		{"handoff refused", func(t *testing.T, f *reapFixture) {
			f.handoff = handoffBad
			archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
		}},
		{"busy session", func(t *testing.T, f *reapFixture) {
			f.runner.idle = false
			archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
		}},
		{"parked at prompt", func(t *testing.T, f *reapFixture) {
			f.runner.blocked = true
			archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
		}},
		{"pinned role", func(t *testing.T, f *reapFixture) {
			_, r := archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
			testutil.NoError(t, f.d.PinHeraRole(r.ID))
		}},
		{"pinned task", func(t *testing.T, f *reapFixture) {
			task, _ := archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
			testutil.NoError(t, f.d.SetPinned(task.ID, true))
		}},
		{"blocked role status", func(t *testing.T, f *reapFixture) {
			_, r := archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
			testutil.NoError(t, f.d.UpsertHeraRoleStatus(r.ID, db.HeraStatusBlocked))
		}},
		{"only coordinators", func(t *testing.T, f *reapFixture) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReapFixture(t)
			tc.setup(t, f)
			f.nudger().Sweep()
			testutil.Equal(t, len(f.inbox(t)), 0)
			testutil.Equal(t, len(f.sent), 0)
		})
	}
}

func TestReapNudge_DeadSessionCountsAsIdle(t *testing.T) {
	f := newReapFixture(t)
	f.runner.alive, f.runner.idle = false, false
	archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
	f.nudger().Sweep()
	testutil.Equal(t, len(f.inbox(t)), 1)
}

func TestReapNudge_DedupSurvivesRestartAndCooldownRenudges(t *testing.T) {
	f := newReapFixture(t)
	archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
	n := f.nudger()
	n.Sweep()
	f.clock = f.clock.Add(time.Hour) // past the coordinator gap and re-eval interval
	n.Sweep()
	testutil.Equal(t, len(f.inbox(t)), 1)

	// A daemon restart is a fresh nudger with empty memory: the persisted
	// record still suppresses the repeat.
	f.nudger().Sweep()
	testutil.Equal(t, len(f.inbox(t)), 1)

	f.clock = f.clock.Add(reapRenudgeCooldown)
	f.nudger().Sweep()
	testutil.Equal(t, len(f.inbox(t)), 2)
}

func TestReapNudge_KindChangeRenudges(t *testing.T) {
	f := newReapFixture(t)
	task, _ := archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
	n := f.nudger()
	n.Sweep()
	testutil.Equal(t, len(f.inbox(t)), 1)

	testutil.NoError(t, f.d.SetStatus(task.ID, model.StatusComplete))
	f.git = gitOK
	f.clock = f.clock.Add(reapCoordGap)
	n.Sweep()
	msgs := f.inbox(t)
	testutil.Equal(t, len(msgs), 2)
	testutil.Contains(t, msgs[1].Body, "w (merged)")
}

func TestReapNudge_CoordinatorGapHoldsThenBatches(t *testing.T) {
	f := newReapFixture(t)
	archWorker(t, f.d, f.orch.ID, "a", model.StatusInReview)
	n := f.nudger()
	n.Sweep()
	testutil.Equal(t, len(f.inbox(t)), 1)

	archWorker(t, f.d, f.orch.ID, "b", model.StatusInReview)
	f.clock = f.clock.Add(reapCoordGap / 2)
	n.Sweep()
	testutil.Equal(t, len(f.inbox(t)), 1) // held during the gap

	f.clock = f.clock.Add(reapCoordGap)
	n.Sweep()
	msgs := f.inbox(t)
	testutil.Equal(t, len(msgs), 2)
	testutil.Equal(t, msgs[1].Tldr, "1 role ready to archive: b")
}

func TestReapNudge_NoLiveCoordinatorHoldsWithoutRecording(t *testing.T) {
	f := newReapFixture(t)
	orch, err := f.d.CreateHeraOrchestrator("orphan", "master")
	testutil.NoError(t, err)
	task, _ := archWorker(t, f.d, orch.ID, "w", model.StatusInReview)
	n := f.nudger()
	n.Sweep()
	testutil.Equal(t, len(f.sent), 0)
	meta, err := f.d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[task.ID][HeraMetaKeyReapNudged], "")

	c := reapCoord(t, f.d, orch.ID, "late-coord")
	n.Sweep()
	testutil.Equal(t, len(f.sent), 1)
	testutil.Equal(t, f.sent[0].ToRoleID, c.ID)
}

func TestReapNudge_ReevalThrottleAndFingerprint(t *testing.T) {
	f := newReapFixture(t)
	f.handoff = func(*model.Task) (HandoffInfo, bool, string) { f.checks++; return HandoffInfo{}, false, "CI failing" }
	task, _ := archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
	n := f.nudger()
	n.Sweep()
	n.Sweep()
	testutil.Equal(t, f.checks, 1) // unchanged fingerprint: cached refusal

	testutil.NoError(t, f.d.SetMeta(task.ID, "pr", "state", "open"))
	n.Sweep()
	testutil.Equal(t, f.checks, 2) // PR state change re-evaluates

	f.clock = f.clock.Add(reapReevalInterval)
	n.Sweep()
	testutil.Equal(t, f.checks, 3) // throttle window elapsed
}

func TestReapNudge_SweepBudgetDefersRemainingRoles(t *testing.T) {
	f := newReapFixture(t)
	f.handoff = func(t *model.Task) (HandoffInfo, bool, string) {
		f.checks++
		f.clock = f.clock.Add(reapSweepBudget + time.Second)
		return handoffOK(t)
	}
	archWorker(t, f.d, f.orch.ID, "a", model.StatusInReview)
	archWorker(t, f.d, f.orch.ID, "b", model.StatusInReview)
	n := f.nudger()
	n.Sweep()
	testutil.Equal(t, f.checks, 1)
	msgs := f.inbox(t)
	testutil.Equal(t, len(msgs), 1)
	testutil.Equal(t, strings.HasPrefix(msgs[0].Tldr, "1 role ready to archive: "), true)
}

type reapFailStore struct {
	*db.DB
	failMeta, failOrchs, failSend bool
}

func (s *reapFailStore) ListMetaByNamespace(ns string) (map[string]map[string]string, error) {
	if s.failMeta {
		return nil, errors.New("boom")
	}
	return s.DB.ListMetaByNamespace(ns)
}

func (s *reapFailStore) ListHeraOrchestrators(a bool) ([]*db.HeraOrchestrator, error) {
	if s.failOrchs {
		return nil, errors.New("boom")
	}
	return s.DB.ListHeraOrchestrators(a)
}

func (s *reapFailStore) SendHeraSystemNotice(id int64, body, tldr string) (*db.HeraMessage, error) {
	if s.failSend {
		return nil, errors.New("boom")
	}
	return s.DB.SendHeraSystemNotice(id, body, tldr)
}

func TestReapNudge_FailsClosedOnLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store reapFailStore
	}{
		{"meta", reapFailStore{failMeta: true}},
		{"orchestrators", reapFailStore{failOrchs: true}},
		{"send", reapFailStore{failSend: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReapFixture(t)
			task, _ := archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
			s := tc.store
			s.DB = f.d
			n := NewReapNudger(&s, f.runner, gitBad, handoffOK, func(m *db.HeraMessage) { f.sent = append(f.sent, m) })
			n.Sweep()
			testutil.Equal(t, len(f.sent), 0)
			meta, err := f.d.ListMetaByNamespace(db.HeraMetaNamespace)
			testutil.NoError(t, err)
			testutil.Equal(t, meta[task.ID][HeraMetaKeyReapNudged], "")
		})
	}
}

func TestReapNudgeText_TldrFitsCap(t *testing.T) {
	var items []reapItem
	for _, name := range []string{"a-very-long-worker-name-one", "a-very-long-worker-name-two", "a-very-long-worker-name-three", "a-very-long-worker-name-four", "a-very-long-worker-name-five"} {
		items = append(items, reapItem{role: &db.HeraRole{Name: name}, kind: db.ArchiveKindHandedOff})
	}
	body, tldr := reapNudgeText(items)
	testutil.Equal(t, len(tldr) <= db.HeraMaxTldrLen, true)
	testutil.Equal(t, strings.HasSuffix(tldr, "…"), true)
	testutil.Contains(t, body, "5 roles ready to archive")
	testutil.Contains(t, body, "a-very-long-worker-name-five (handed off)")
}

func TestReapNudge_AlreadyNudgedParsing(t *testing.T) {
	n := &ReapNudger{cooldown: time.Hour}
	now := time.Unix(10_000, 0)
	testutil.Equal(t, n.alreadyNudged("", "x", now), false)
	testutil.Equal(t, n.alreadyNudged("x|garbage", "x", now), false)
	testutil.Equal(t, n.alreadyNudged("y|9999", "x", now), false)
	testutil.Equal(t, n.alreadyNudged("x|9999", "x", now), true)
	testutil.Equal(t, n.alreadyNudged("x|1", "x", now), false)
}

func TestReapNudge_PokeRunsDebouncedSweep(t *testing.T) {
	f := newReapFixture(t)
	archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
	n := f.nudger()
	n.sweepInterval = time.Hour
	n.debounce = 10 * time.Millisecond
	var nilNudger *ReapNudger
	nilNudger.Poke() // nil-safe
	done := make(chan struct{})
	go func() { n.Start(); close(done) }()
	n.Poke()
	n.Poke() // coalesces
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		got := len(f.sent)
		f.mu.Unlock()
		if got == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no nudge after poke")
		}
		time.Sleep(5 * time.Millisecond)
	}
	n.Stop()
	n.Stop() // idempotent
	<-done
}

func TestReapNudge_PeriodicSweep(t *testing.T) {
	f := newReapFixture(t)
	archWorker(t, f.d, f.orch.ID, "w", model.StatusInReview)
	n := f.nudger()
	n.sweepInterval = 10 * time.Millisecond
	n.debounce = time.Hour
	done := make(chan struct{})
	go func() { n.Start(); close(done) }()
	n.Poke() // arms a debounce timer that Stop must cancel
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		got := len(f.sent)
		f.mu.Unlock()
		if got >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no periodic nudge")
		}
		time.Sleep(5 * time.Millisecond)
	}
	n.Stop()
	<-done
}
