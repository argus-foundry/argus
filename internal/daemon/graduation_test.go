package daemon

import (
	"fmt"
	"testing"

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
	g = newGraduationWatcher(d, &graduationRunner{}, d.Config, n)
	n.state = notify.StateSubmitted
	g.sweep()
	meta, err := d.ListMetaByNamespace(db.HeraMetaNamespace)
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
func (s *graduationSession) IsIdle() bool        { return s.idle }
func (s *graduationSession) PID() int            { return s.pid }
func (s *graduationSession) PTYSize() (int, int) { return 100, 30 }

type graduationRunner struct {
	agent.SessionRunner
	sess          *graduationSession
	pending       bool
	kicks, starts int
	recycles      int
	launched      model.Task
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
	_, b, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "mission"}, w.ID, w.Worktree)
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
	newWatcher := func() *graduationWatcher {
		g := newGraduationWatcher(d, r, d.Config, nil)
		g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
		g.stopJobs = func(*model.Task, config.Config, string) error { jobs++; return nil }
		return g
	}
	g := newWatcher()
	for range 60 {
		g.sweep()
	} // no deadline, no job cleanup while busy
	testutil.Equal(t, r.kicks, 0)
	testutil.Equal(t, jobs, 0)
	g = newWatcher() // daemon bounce: all retry state is in the DB
	r.sess.idle = true
	g.sweep()
	testutil.Equal(t, r.kicks, 1)
	testutil.Equal(t, jobs, 1)
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
