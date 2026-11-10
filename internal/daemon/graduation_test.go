package daemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/testutil"
)

type graduationSession struct {
	agent.SessionHandle
	idle bool
	pid  int
	// newPID, once refreshed by IsIdle/PTYSize, replaces pid: models a
	// RemoteSession whose cached PID is stale until refreshInfo runs.
	newPID    int
	refreshed bool
}

type graduationNotice struct {
	body, id string
	state    notify.DeliveryState
}

func (n *graduationNotice) ReliableNotify(_, body, id string, _ notify.NotifyOpts) func() {
	n.body, n.id = body, id
	return func() {}
}
func (n *graduationNotice) DeliveryState(string, string) notify.DeliveryState { return n.state }

func TestGraduationOrientationOutboxSurvivesBounce(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	testutil.NoError(t, d.CompleteHeraGraduation(w.ID, "[argus] orientation"))
	n := &graduationNotice{}
	g := newGraduationWatcher(d, &graduationRunner{}, d.Config, n)
	g.sweep()
	testutil.Equal(t, n.id, "graduation:"+w.ID)
	testutil.Equal(t, n.body, "[argus] orientation")
	meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[w.ID]["graduation_orientation"], "[argus] orientation")
	n.id, n.body = "", ""
	g = newGraduationWatcher(d, &graduationRunner{}, d.Config, n)
	n.state = notify.StatePending
	g.sweep()
	testutil.Equal(t, n.id, "graduation:"+w.ID)
	testutil.Equal(t, n.body, "[argus] orientation")
	meta, err = d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[w.ID]["graduation_orientation"], "[argus] orientation")
	g = newGraduationWatcher(d, &graduationRunner{}, d.Config, n)
	n.state = notify.StateSubmitted
	g.sweep()
	meta, err = d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[w.ID]["graduation_orientation"], "")
	msgs, err := d.Inbox(w.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
}

func TestGraduationPreflightAndNoJobsOnRefusal(t *testing.T) {
	for _, tc := range []string{"no id", "busy", "pending restart", "missing transcript", "unreadable transcript", "absent session"} {
		t.Run(tc, func(t *testing.T) {
			d, w, _ := graduatedFixture(t)
			r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
			g := newGraduationWatcher(d, r, d.Config, nil)
			g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
			switch tc {
			case "no id":
				w.SessionID = ""
			case "busy":
				r.sess.idle = false
			case "pending restart":
				r.pending = true
			case "missing transcript":
				g.resumable = func(*model.Task, config.Config) (bool, error) { return false, nil }
			case "unreadable transcript":
				g.resumable = func(*model.Task, config.Config) (bool, error) { return false, fmt.Errorf("permission denied") }
			case "absent session":
				r.sess = nil
			}
			testutil.Equal(t, g.preflight(w) != nil, tc != "absent session")
			if tc != "absent session" {
				testutil.NoError(t, d.Update(w))
				g.stopJobs = func(*model.Task, config.Config, string) error { t.Fatal("jobs touched on refusal"); return nil }
				g.sweep()
				testutil.Equal(t, r.kicks, 0)
			}
		})
	}
}

func (s *graduationSession) Alive() bool         { return true }
func (s *graduationSession) IsIdle() bool        { s.refreshed = true; return s.idle }
func (s *graduationSession) PTYSize() (int, int) { s.refreshed = true; return 100, 30 }
func (s *graduationSession) PID() int {
	if s.newPID != 0 && s.refreshed {
		return s.newPID
	}
	return s.pid
}

type graduationRunner struct {
	agent.SessionRunner
	sess          *graduationSession
	pending       bool
	kicks, starts int
	recycles      int
	launched      model.Task
	onKick        func()
}

func (r *graduationRunner) Recycle(t *model.Task, _ config.Config, _, _ uint16) error {
	r.recycles++
	r.launched = *t
	return nil
}

func TestGraduateOrdinaryCoordinatorRecycle(t *testing.T) {
	d, w, result := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	rr := NewHeraRecycleRunner(d, r, d.Config) // no resident runner injected
	err := rr.Restart(w.ID, result.NewRole.ID)
	testutil.Contains(t, err.Error(), "graduation pending restart")
	testutil.NoError(t, d.CompleteHeraGraduation(w.ID, "orientation"))
	testutil.NoError(t, rr.Restart(w.ID, result.NewRole.ID))
	testutil.Equal(t, r.recycles, 1)
	testutil.True(t, r.launched.SessionID != w.SessionID)
	testutil.Contains(t, r.launched.Prompt, "mission")
}
func (r *graduationRunner) Get(string) agent.SessionHandle {
	if r.sess == nil {
		return nil
	}
	return r.sess
}
func (r *graduationRunner) HasPendingRestart(string) bool { return r.pending }
func (r *graduationRunner) KickRerender(task *model.Task, _ config.Config, _, _ uint16) error {
	r.kicks++
	if r.onKick != nil {
		r.onKick()
	}
	r.launched = *task
	r.pending = true
	return nil
}
func (r *graduationRunner) StartOrReattach(task *model.Task, _ config.Config, _, _ uint16, resume bool) (agent.SessionHandle, bool, error) {
	if !resume {
		panic("graduation must resume")
	}
	r.starts++
	r.launched = *task
	r.sess = &graduationSession{idle: true, pid: 22}
	return r.sess, false, nil
}

func graduatedFixture(t *testing.T) (*db.DB, *model.Task, *db.MoveHeraBindingResult) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "work", Name: "work", Backend: "claude", SessionID: "same-conversation", Project: "p", Worktree: t.TempDir(), Branch: "argus/resident/work", Account: "routed", AccountRouted: true}
	testutil.NoError(t, d.Add(w))
	_, b, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "mission"}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetMeta(w.ID, db.HeraMetaNamespace, db.HeraMetaKeyResidentWorker, "true"))
	result, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	return d, w, result
}

func TestGraduationRestartRetryAndBounce(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{pid: 11}}
	jobs := 0
	flipBusy := false
	newWatcher := func() *graduationWatcher {
		g := newGraduationWatcher(d, r, d.Config, nil)
		g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
		g.stopJobs = func(*model.Task, config.Config, string) error {
			jobs++
			if flipBusy {
				r.sess.idle = false
			}
			return nil
		}
		return g
	}
	r.onKick = func() {
		meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
		testutil.NoError(t, err)
		testutil.Equal(t, meta[w.ID][db.HeraMetaKeyGraduationPendingRestart], "restart:11")
	}
	g := newWatcher()
	for range 60 {
		g.sweep()
	} // no deadline, no job cleanup while busy
	testutil.Equal(t, r.kicks, 0)
	testutil.Equal(t, jobs, 0)
	g = newWatcher() // daemon bounce: all retry state is in the DB
	r.sess.idle = true
	flipBusy = true // session turns busy while stray jobs are cleaned: no kick
	g.sweep()
	testutil.Equal(t, r.kicks, 0)
	testutil.Equal(t, jobs, 1)
	meta0, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta0[w.ID][db.HeraMetaKeyGraduationPendingRestart], "restart:11")
	flipBusy = false
	r.sess.idle = true
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
	testutil.Equal(t, jobs, 2)
	testutil.Equal(t, r.launched.SessionID, w.SessionID)
	testutil.Equal(t, r.launched.Worktree, w.Worktree)
	testutil.Equal(t, r.launched.Branch, w.Branch)
	testutil.Equal(t, r.launched.Prompt, w.Prompt)
	testutil.True(t, r.launched.AccountRouted)
	testutil.Equal(t, r.launched.Account, w.Account)
	g = newWatcher()
	g.sweep() // crash after kick was queued: do not repeat
	testutil.Equal(t, r.kicks, 1)
	r.pending = false
	r.sess.pid = 22
	g.sweep()
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
	meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[w.ID][db.HeraMetaKeyGraduationPendingRestart], "")
	msgs, err := d.Inbox(w.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	testutil.Contains(t, msgs[0].Body, "top-level coordinator")
	testutil.Contains(t, msgs[0].Body, "hera_spawn_worker")
}

func TestGraduationCrashBeforeKickAndDeadResume(t *testing.T) {
	for _, dead := range []bool{false, true} {
		t.Run(map[bool]string{true: "dead", false: "before kick"}[dead], func(t *testing.T) {
			d, w, _ := graduatedFixture(t)
			testutil.NoError(t, d.SetMeta(w.ID, db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "restart:11"))
			r := &graduationRunner{}
			if !dead {
				r.sess = &graduationSession{idle: true, pid: 11}
			}
			g := newGraduationWatcher(d, r, d.Config, nil)
			g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
			g.stopJobs = func(*model.Task, config.Config, string) error { return nil }
			g.sweep()
			if dead {
				testutil.Equal(t, r.starts, 1)
			} else {
				testutil.Equal(t, r.kicks, 1)
			}
			testutil.Equal(t, r.launched.SessionID, w.SessionID)
		})
	}
}

func TestGraduationStalePIDDoesNotDoubleKick(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	jobs := 0
	g := newGraduationWatcher(d, r, d.Config, nil)
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	g.stopJobs = func(*model.Task, config.Config, string) error { jobs++; return nil }
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
	// The supervisor resumed the session under a new PID, but the handle's
	// cached PID is stale until IsIdle/PTYSize refreshes it.
	r.pending = false
	r.sess.newPID, r.sess.refreshed = 22, false
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
	testutil.Equal(t, jobs, 1)
	meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[w.ID][db.HeraMetaKeyGraduationPendingRestart], "")
	msgs, err := d.Inbox(w.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
}

func TestGraduationDeadResumeBackoff(t *testing.T) {
	d, _, _ := graduatedFixture(t)
	r := &graduationRunner{}
	g := newGraduationWatcher(d, r, d.Config, nil)
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	g.stopJobs = func(*model.Task, config.Config, string) error { return nil }
	clock := time.Unix(1000, 0)
	g.now = func() time.Time { return clock }
	g.sweep()
	testutil.Equal(t, r.starts, 1)
	// The resumed session dies at once: rapid sweeps must not re-resume.
	r.sess = nil
	for range 5 {
		clock = clock.Add(time.Second)
		g.sweep()
	}
	testutil.Equal(t, r.starts, 1)
	clock = clock.Add(graduationResumeBackoff)
	g.sweep()
	testutil.Equal(t, r.starts, 2)
}

func TestGraduationRecordedBindingSurvivesSecondLiveBinding(t *testing.T) {
	d, w, res := graduatedFixture(t)
	meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[w.ID][db.HeraMetaKeyGraduationBindingID], fmt.Sprint(res.NewBinding.ID))
	// A second live binding makes the by-task lookup ambiguous forever.
	o2, err := d.CreateHeraOrchestrator("other", "")
	testutil.NoError(t, err)
	r2, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o2.ID, Name: "w2", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "x"})
	testutil.NoError(t, err)
	_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: r2.ID, OrchestratorID: o2.ID, ArgusTaskID: w.ID, WorktreePath: w.Worktree})
	testutil.NoError(t, err)
	_, err = d.HeraLiveBindingByTask(w.ID)
	testutil.ErrorIs(t, err, db.ErrHeraAmbiguous)

	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	g := newGraduationWatcher(d, r, d.Config, nil)
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	g.stopJobs = func(*model.Task, config.Config, string) error { return nil }
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
	r.pending = false
	r.sess.pid = 22
	g.sweep()
	testutil.False(t, d.GraduationPending(w.ID))
}

func TestGraduationEndedBindingAbandonsState(t *testing.T) {
	for _, noID := range []bool{false, true} {
		t.Run(fmt.Sprint("noID=", noID), func(t *testing.T) {
			d, w, res := graduatedFixture(t)
			if noID {
				testutil.NoError(t, d.SetMeta(w.ID, db.HeraMetaNamespace, db.HeraMetaKeyGraduationBindingID, ""))
			} else {
				testutil.NoError(t, d.EndHeraBinding(res.NewBinding.ID, db.HeraEndReasonUserDeleted))
			}
			r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
			g := newGraduationWatcher(d, r, d.Config, nil)
			g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
			g.stopJobs = func(*model.Task, config.Config, string) error { return nil }
			g.sweep()
			// A pending row without a recorded id is cleared, never guessed.
			testutil.Equal(t, r.kicks, 0)
			testutil.False(t, d.GraduationPending(w.ID))
			meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
			testutil.NoError(t, err)
			testutil.Equal(t, meta[w.ID][db.HeraMetaKeyGraduationBindingID], "")
		})
	}
}

// A human abandon landing between the sweep's meta snapshot and advance must
// not be undone: no kick and the marker stays cleared. (Old code fell back to
// the by-task lookup and re-upserted "restart:<pid>".)
func TestGraduationAbandonBetweenSnapshotAndAdvance(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	g := newGraduationWatcher(d, r, d.Config, nil)
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	g.stopJobs = func(*model.Task, config.Config, string) error { return nil }
	testutil.NoError(t, d.AbandonGraduationPending(w.ID))
	testutil.NoError(t, g.advance(w.ID, "true")) // stale phase from the snapshot
	testutil.Equal(t, r.kicks, 0)
	testutil.False(t, d.GraduationPending(w.ID))
}

// Abandon after resolveGraduate but before the phase write: the CAS refuses.
func TestGraduationAbandonDuringJobCleanup(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	g := newGraduationWatcher(d, r, d.Config, nil)
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	g.stopJobs = func(*model.Task, config.Config, string) error {
		return d.AbandonGraduationPending(w.ID)
	}
	g.sweep()
	testutil.Equal(t, r.kicks, 0)
	testutil.False(t, d.GraduationPending(w.ID))
}

func TestGraduationPersistentPreflightFailureIsBackedOffAndLoggedOnce(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	g := newGraduationWatcher(d, r, d.Config, nil)
	probes := 0
	g.resumable = func(*model.Task, config.Config) (bool, error) { probes++; return false, nil }
	g.stopJobs = func(*model.Task, config.Config, string) error { return nil }
	var logs []string
	g.logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	clock := time.Unix(1000, 0)
	g.now = func() time.Time { return clock }
	for range 25 { // 25s of 1s ticks = 3 backoff windows
		g.sweep()
		clock = clock.Add(time.Second)
	}
	testutil.Equal(t, probes, 3)
	testutil.Equal(t, len(logs), 1)
	testutil.True(t, d.GraduationPending(w.ID)) // still no deadline
	// A different reason logs again.
	g.resumable = func(*model.Task, config.Config) (bool, error) { probes++; return false, fmt.Errorf("boom") }
	clock = clock.Add(graduationResumeBackoff)
	g.sweep()
	g.sweep()
	testutil.Equal(t, len(logs), 2)
	// Recovery resumes immediately after the window.
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	clock = clock.Add(graduationResumeBackoff)
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
}
