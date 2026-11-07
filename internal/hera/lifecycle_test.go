package hera

import (
	"errors"
	"sync"
	"testing"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/testutil"
)

type fakeLifecycleSessions struct {
	alive    map[string]bool
	writes   []string
	writeErr error
}

func (f *fakeLifecycleSessions) SessionAlive(taskID string) bool { return f.alive[taskID] }
func (f *fakeLifecycleSessions) SendSystemInput(taskID, text string) error {
	f.writes = append(f.writes, taskID+":"+text)
	return f.writeErr
}

type lifecycleEvents struct {
	mu  sync.Mutex
	got []LifecycleOperation
}

func (e *lifecycleEvents) emit(_ string, p LifecycleOperation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, p)
}

func newLifecycle(t *testing.T, d *db.DB, taskID string, alive bool) (*LifecycleService, *fakeLifecycleSessions, *fakeRecycleRunner, *lifecycleEvents) {
	t.Helper()
	sess := &fakeLifecycleSessions{alive: map[string]bool{taskID: alive}}
	rr := &fakeRecycleRunner{}
	ev := &lifecycleEvents{}
	svc := NewLifecycleService(d, sess, rr)
	svc.SetSynchronous()
	svc.SetEmitter(ev.emit)
	return svc, sess, rr, ev
}

const (
	reqA = "11111111-1111-4111-8111-111111111111"
	reqB = "22222222-2222-4222-8222-222222222222"
)

func TestLifecycle_Bounce(t *testing.T) {
	t.Run("writes one instruction, never kills, stays awaiting", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleRole(t, d, db.HeraKindWorker, "o", "w", "/wt/w", "b", "m")
		svc, sess, rr, ev := newLifecycle(t, d, task.ID, true)

		op, err := svc.Bounce(reqA, role.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, op.State, LifecycleStateAwaitingHandoff)
		testutil.Equal(t, len(sess.writes), 1)
		testutil.Equal(t, sess.writes[0], task.ID+":"+BounceInstruction)
		testutil.Equal(t, len(rr.calls), 0)

		again, err := svc.Bounce(reqA, role.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, again.ID, op.ID)
		testutil.Equal(t, len(sess.writes), 1)
		testutil.Equal(t, len(ev.got), 1)
	})

	t.Run("freelance is allowed", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleRole(t, d, db.HeraKindFreelance, "o", "f", "/wt/f", "b", "m")
		svc, sess, _, _ := newLifecycle(t, d, task.ID, true)
		_, err := svc.Bounce(reqA, role.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, len(sess.writes), 1)
	})

	t.Run("rejects coordinator, dead session, stale binding, missing role", func(t *testing.T) {
		d := openRecycleDB(t)
		ctask, _, coord := seedRecycleCoordinator(t, d, "oc", "/wt/c", "b", "m")
		svc, sess, _, _ := newLifecycle(t, d, ctask.ID, true)
		_, err := svc.Bounce(reqA, coord.ID)
		testutil.ErrorIs(t, err, ErrLifecycleWrongKind)

		wtask, _, worker := seedRecycleRole(t, d, db.HeraKindWorker, "ow", "w", "/wt/w", "b", "m")
		sess.alive[wtask.ID] = false
		_, err = svc.Bounce(reqA, worker.ID)
		testutil.ErrorIs(t, err, ErrLifecycleNoSession)

		sess.alive[wtask.ID] = true
		b, err := d.HeraLiveBindingByRole(worker.ID)
		testutil.NoError(t, err)
		testutil.NoError(t, d.EndHeraBinding(b.ID, "test"))
		_, err = svc.Bounce(reqA, worker.ID)
		testutil.ErrorIs(t, err, ErrLifecycleStaleBinding)

		_, err = svc.Bounce(reqA, 9999)
		testutil.ErrorIs(t, err, db.ErrHeraNotFound)
		testutil.Equal(t, len(sess.writes), 0)
	})

	t.Run("request reuse and concurrent operation conflict", func(t *testing.T) {
		d := openRecycleDB(t)
		t1, _, w1 := seedRecycleRole(t, d, db.HeraKindWorker, "o1", "w1", "/wt/1", "b", "m")
		t2, _, w2 := seedRecycleRole(t, d, db.HeraKindWorker, "o2", "w2", "/wt/2", "b", "m")
		svc, sess, _, _ := newLifecycle(t, d, t1.ID, true)
		sess.alive[t2.ID] = true

		_, err := svc.Bounce(reqA, w1.ID)
		testutil.NoError(t, err)
		_, err = svc.Bounce(reqA, w2.ID)
		testutil.ErrorIs(t, err, ErrLifecycleRequestReuse)
		_, err = svc.Bounce(reqB, w1.ID)
		testutil.ErrorIs(t, err, ErrLifecycleActive)
		testutil.Equal(t, len(sess.writes), 1)
	})

	t.Run("write failure becomes a failed operation", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleRole(t, d, db.HeraKindWorker, "o", "w", "/wt/w", "b", "m")
		svc, sess, _, ev := newLifecycle(t, d, task.ID, true)
		sess.writeErr = errors.New("pty closed secret-path")

		op, err := svc.Bounce(reqA, role.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, op.State, LifecycleStateFailed)
		testutil.True(t, op.Error != "" && op.Error != sess.writeErr.Error())
		testutil.Equal(t, ev.got[len(ev.got)-1].State, LifecycleStateFailed)
		testutil.True(t, ev.got[len(ev.got)-1].Error != nil)
	})

	t.Run("self-service recycle observed advances to restarted", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleRole(t, d, db.HeraKindWorker, "o", "w", "/wt/w", "b", "m")
		svc, _, _, ev := newLifecycle(t, d, task.ID, true)
		op, err := svc.Bounce(reqA, role.ID)
		testutil.NoError(t, err)

		svc.ObserveRecycle(9999, nil)
		got, _ := svc.Get(role.ID, op.ID)
		testutil.Equal(t, got.State, LifecycleStateAwaitingHandoff)

		svc.ObserveRecycle(role.ID, nil)
		got, err = svc.Get(role.ID, op.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.State, LifecycleStateRestarted)
		testutil.Equal(t, ev.got[len(ev.got)-1].State, LifecycleStateRestarted)

		svc.ObserveRecycle(role.ID, nil) // no active op: no-op, no extra event
		testutil.Equal(t, len(ev.got), 2)
	})

	t.Run("observed recycle failure marks failed", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleRole(t, d, db.HeraKindWorker, "o", "w", "/wt/w", "b", "m")
		svc, _, _, _ := newLifecycle(t, d, task.ID, true)
		op, err := svc.Bounce(reqA, role.ID)
		testutil.NoError(t, err)
		svc.ObserveRecycle(role.ID, errors.New("boom"))
		got, _ := svc.Get(role.ID, op.ID)
		testutil.Equal(t, got.State, LifecycleStateFailed)
	})
}

func TestLifecycle_ForceRecycle(t *testing.T) {
	t.Run("restarts immediately and preserves identity", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleCoordinator(t, d, "o", "/wt/c", "argus/c", "mission")
		svc, _, rr, ev := newLifecycle(t, d, task.ID, true)

		op, err := svc.ForceRecycle(reqA, role.ID, true)
		testutil.NoError(t, err)
		testutil.Equal(t, op.State, LifecycleStateRestarted)
		testutil.Equal(t, rr.restartTaskID, task.ID)
		testutil.Equal(t, rr.restartRoleID, role.ID)
		testutil.Equal(t, len(ev.got), 2)
		testutil.Equal(t, ev.got[0].State, LifecycleStateRestarting)

		got, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Worktree, "/wt/c")
		testutil.Equal(t, got.Branch, "argus/c")
	})

	t.Run("seed prompt carries mission and handoff", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleCoordinator(t, d, "o", "/wt/c", "b", "the mission text")
		testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyHandoffNote, "handoff text"))
		seed, err := BuildRecycleSeedPrompt(d, role.ID)
		testutil.NoError(t, err)
		testutil.Contains(t, seed, "the mission text")
		testutil.Contains(t, seed, "handoff text")
	})

	t.Run("runtime failure is a failed operation with a safe error", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleCoordinator(t, d, "o", "/wt/c", "b", "m")
		svc, _, rr, _ := newLifecycle(t, d, task.ID, true)
		rr.restartErr = errors.New("spawn /secret/path failed")

		op, err := svc.ForceRecycle(reqA, role.ID, true)
		testutil.NoError(t, err)
		testutil.Equal(t, op.State, LifecycleStateFailed)
		testutil.True(t, op.Error != "" && op.Error != rr.restartErr.Error())
	})

	t.Run("rejects worker and dead session; replay does not restart again", func(t *testing.T) {
		d := openRecycleDB(t)
		wtask, _, worker := seedRecycleRole(t, d, db.HeraKindWorker, "ow", "w", "/wt/w", "b", "m")
		svc, sess, rr, _ := newLifecycle(t, d, wtask.ID, true)
		_, err := svc.ForceRecycle(reqA, worker.ID, true)
		testutil.ErrorIs(t, err, ErrLifecycleWrongKind)

		ctask, _, coord := seedRecycleCoordinator(t, d, "oc", "/wt/c", "b", "m")
		_, err = svc.ForceRecycle(reqA, coord.ID, true)
		testutil.ErrorIs(t, err, ErrLifecycleNoSession)

		sess.alive[ctask.ID] = true
		op, err := svc.ForceRecycle(reqA, coord.ID, true)
		testutil.NoError(t, err)
		again, err := svc.ForceRecycle(reqA, coord.ID, true)
		testutil.NoError(t, err)
		testutil.Equal(t, again.ID, op.ID)
		testutil.Equal(t, len(rr.calls), 2) // one stop_stray + one restart
	})

	t.Run("async path completes in the background", func(t *testing.T) {
		d := openRecycleDB(t)
		task, _, role := seedRecycleCoordinator(t, d, "o", "/wt/c", "b", "m")
		sess := &fakeLifecycleSessions{alive: map[string]bool{task.ID: true}}
		done := make(chan struct{})
		svc := NewLifecycleService(d, sess, &fakeRecycleRunner{})
		svc.SetEmitter(func(_ string, p LifecycleOperation) {
			if p.State == LifecycleStateRestarted {
				close(done)
			}
		})
		op, err := svc.ForceRecycle(reqA, role.ID, false)
		testutil.NoError(t, err)
		testutil.Equal(t, op.State, LifecycleStateRestarting)
		<-done
	})
}

func TestLifecycle_RecoverAndGet(t *testing.T) {
	d := openRecycleDB(t)
	task, _, role := seedRecycleCoordinator(t, d, "o", "/wt/c", "b", "m")
	svc, _, _, ev := newLifecycle(t, d, task.ID, true)
	op, _, err := d.CreateOperation(&db.Operation{ID: NewOperationID(), RequestID: reqA, Fingerprint: lifecycleFingerprint(LifecycleKindForceRecycle, role.ID),
		Kind: LifecycleKindForceRecycle, SubjectType: lifecycleSubjectRole, SubjectID: "1", TaskID: task.ID, State: LifecycleStateRestarting})
	testutil.NoError(t, err)

	svc.Recover()
	got, err := d.Operation(op.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.State, LifecycleStateFailed)
	testutil.Equal(t, len(ev.got), 1)

	_, err = svc.Get(role.ID+100, op.ID)
	testutil.ErrorIs(t, err, db.ErrOperationNotFound)
	testutil.Equal(t, len(NewOperationID()), 36)
}

func openRecycleDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}
