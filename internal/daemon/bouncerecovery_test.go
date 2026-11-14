package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bouncerecovery"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestReattach_ConsumesSupervisorLivenessSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	task := &model.Task{Name: "pending-live", Status: model.StatusPending}
	testutil.NoError(t, d.db.Add(task))
	testutil.NoError(t, os.MkdirAll(db.DataDir(), 0o700))
	data, err := json.Marshal([]string{task.ID})
	testutil.NoError(t, err)
	path := filepath.Join(db.DataDir(), supervisorLiveTasksFilename)
	testutil.NoError(t, os.WriteFile(path, data, 0o600))
	sup := &fakeSupClient{running: nil}
	d.UseSupervisorRunner(sup)
	d.ReconcileOnStartup()
	_, err = os.Stat(path)
	testutil.NoError(t, err) // failed query must retain evidence
	sup.running = []string{}
	d.ReconcileOnStartup()
	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	testutil.NotNil(t, got[task.ID])
	_, err = os.Stat(path)
	testutil.True(t, os.IsNotExist(err))
}

type snapshotShutdownRunner struct {
	agent.SessionRunner
	ids []string
}

func (r *snapshotShutdownRunner) Running() []string { return r.ids }
func (r *snapshotShutdownRunner) StopAll()          { r.ids = nil }

func TestSupervisorCleanup_RecordsLiveTasksBeforeStop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := NewSupervisor(nil)
	dir := t.TempDir()
	s.sockPath, s.pidPath = filepath.Join(dir, "s.sock"), filepath.Join(dir, "s.pid")
	s.runner = &snapshotShutdownRunner{ids: []string{"pending-live"}}
	s.cleanup()
	data, err := os.ReadFile(filepath.Join(dir, supervisorLiveTasksFilename))
	testutil.NoError(t, err)
	var ids []string
	testutil.NoError(t, json.Unmarshal(data, &ids))
	testutil.DeepEqual(t, ids, []string{"pending-live"})
	testutil.Equal(t, len(s.runner.Running()), 0)
	s.cleanup()
	data, err = os.ReadFile(filepath.Join(dir, supervisorLiveTasksFilename))
	testutil.NoError(t, err)
	testutil.NoError(t, json.Unmarshal(data, &ids))
	testutil.Equal(t, len(ids), 0) // don't replay evidence from an earlier shutdown
}

func reattachWithOrphan(t *testing.T, d *Daemon, sup *fakeSupClient) *model.Task {
	t.Helper()
	orphan := &model.Task{Name: "orphan", Project: "proj", Status: model.StatusInProgress}
	testutil.NoError(t, d.db.Add(orphan))
	d.UseSupervisorRunner(sup)
	d.ReconcileOnStartup()
	return orphan
}

func TestReattach_RecordsOrphanInRecoveryLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	orphan := reattachWithOrphan(t, d, &fakeSupClient{running: []string{}})

	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	e := got[orphan.ID]
	testutil.Equal(t, e != nil, true)
	testutil.Equal(t, e.Role, bouncerecovery.RolePlain)
	testutil.Equal(t, e.Status, bouncerecovery.StatusPending) // unknown state resumes like mid-turn
	// The legacy signal is unchanged alongside the ledger.
	testutil.Equal(t, hasBounceSignal(t, d, orphan.ID), true)
}

func TestReattach_RecoveryOffWritesNoLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	d.cfgFn = func() config.Config {
		c := config.DefaultConfig()
		c.BounceRecovery.Enabled = false
		return c
	}
	orphan := reattachWithOrphan(t, d, &fakeSupClient{running: []string{}})
	pending := &model.Task{Name: "pending", Status: model.StatusPending}
	testutil.NoError(t, d.db.Add(pending))
	data, err := json.Marshal([]string{pending.ID})
	testutil.NoError(t, err)
	testutil.NoError(t, os.MkdirAll(db.DataDir(), 0o700))
	testutil.NoError(t, os.WriteFile(filepath.Join(db.DataDir(), supervisorLiveTasksFilename), data, 0o600))
	d.ReconcileOnStartup()
	unchanged, err := d.db.Get(pending.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, unchanged.Status, model.StatusPending)

	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)
	testutil.Equal(t, hasBounceSignal(t, d, orphan.ID), true)
}

func TestReattach_LiveSetQueryFailureWritesNoLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	reattachWithOrphan(t, d, &fakeSupClient{running: nil})
	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)
}

func TestBounceRecoveryRunner_ResumeAndSettled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	r := &bounceRecoveryRunner{revive: NewHeraReviveRunner(d.db, d.runner, d.cfgFn), runner: d.runner}
	testutil.Equal(t, r.IsAlive("nope"), false)
	testutil.Equal(t, r.Settled("nope"), false)
	testutil.Equal(t, r.Resume("nope") != nil, true) // unknown task fails loudly
}

func TestBounceRecoveryRunner_RequiresConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	r := &bounceRecoveryRunner{revive: NewHeraReviveRunner(d.db, d.runner, d.cfgFn), runner: d.runner}
	for _, id := range []string{"", "missing"} {
		t.Run("identity="+id, func(t *testing.T) {
			task := &model.Task{Name: "recovery", Backend: "claude", Worktree: t.TempDir(), SessionID: id, Status: model.StatusInReview}
			testutil.NoError(t, d.db.Add(task))
			testutil.ErrorIs(t, r.Resume(task.ID), agent.ErrResumeUnavailable)
			testutil.Nil(t, d.runner.Get(task.ID))
			got, err := d.db.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.SessionID, id)
			testutil.Equal(t, got.Status, model.StatusInReview)
		})
	}
}

type recoveryReceiptRunner struct {
	agent.SessionRunner
	seq     uint64
	pid     int
	sess    agent.SessionHandle
	pending bool
}

func (r *recoveryReceiptRunner) StartReceipt(string) (uint64, int, bool) {
	return r.seq, r.pid, r.pending
}
func (r *recoveryReceiptRunner) Get(string) agent.SessionHandle { return r.sess }

func (r *recoveryReceiptRunner) Start(task *model.Task, _ config.Config, _, _ uint16, _ bool) (agent.SessionHandle, error) {
	// Another caller obtained a sequence during resume validation; this launch
	// must report its actual receipt instead of predicting the next value.
	r.seq += 2
	task.StartSequence = r.seq
	r.pending = true
	return nil, agent.ErrStartAmbiguous
}

func TestBounceRecoveryRunner_CapturesActualLaunchReceipt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := openBounceTestDB(t)
	testutil.NoError(t, d.SetBackend("fixture", config.Backend{Command: "echo"}))
	task := &model.Task{ID: "t", Name: "t", Backend: "fixture", SessionID: "saved", Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	runner := &recoveryReceiptRunner{seq: 10}
	r := &bounceRecoveryRunner{revive: NewHeraReviveRunner(d, runner, d.Config), runner: runner}
	testutil.ErrorIs(t, r.Resume("t"), agent.ErrStartAmbiguous)
	testutil.Equal(t, r.attempts["t"], uint64(12))
	testutil.True(t, r.ResumePending("t"))
	runner.pending = false
	runner.pid, runner.sess = 42, &graduationSession{pid: 42}
	testutil.False(t, r.ResumePending("t"))
	testutil.True(t, r.OwnsLiveSession("t"))
	runner.seq++
	runner.pending = true
	testutil.False(t, r.ResumePending("t"))
	testutil.False(t, r.OwnsLiveSession("t"))
}

func TestBounceRecoveryRunner_ReceiptMustMatchLivePID(t *testing.T) {
	for _, tc := range []struct {
		name string
		seq  uint64
		pid  int
		want bool
	}{
		{"owned", 1, 42, true}, {"different start", 2, 42, false}, {"different pid", 1, 43, false}, {"not confirmed", 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openBounceTestDB(t)
			task := &model.Task{ID: "t", Name: "t", Status: model.StatusInReview}
			testutil.NoError(t, database.Add(task))
			runner := &recoveryReceiptRunner{seq: tc.seq, pid: tc.pid, sess: &graduationSession{pid: 42}}
			r := &bounceRecoveryRunner{revive: NewHeraReviveRunner(database, runner, database.Config), runner: runner, attempts: map[string]uint64{"t": 1}}
			testutil.Equal(t, r.OwnsLiveSession("t"), tc.want)
			got, err := database.Get("t")
			testutil.NoError(t, err)
			if tc.want {
				testutil.Equal(t, got.Status, model.StatusInProgress)
			} else {
				testutil.Equal(t, got.Status, model.StatusInReview)
			}
		})
	}
}
