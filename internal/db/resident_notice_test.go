package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type noticeFixture struct {
	d      *DB
	orch   *HeraOrchestrator
	worker *HeraRole
	task   string
}

func newNoticeFixture(t *testing.T) *noticeFixture {
	t.Helper()
	d := testDB(t)
	rt, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(rt.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Add(&model.Task{ID: "wt", Name: "alpha", Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))
	w, _, err := d.CreateResidentWorkerRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "alpha", Kind: HeraKindWorker, ArgusProject: "p"}, "wt", t.TempDir())
	testutil.NoError(t, err)
	return &noticeFixture{d: d, orch: o, worker: w, task: "wt"}
}

func (f *noticeFixture) pending(t *testing.T) []ResidentNotice {
	t.Helper()
	ns, err := f.d.PendingResidentNotices(50)
	testutil.NoError(t, err)
	return ns
}

func TestResidentNotice_HumanActionsEnqueue(t *testing.T) {
	cases := []struct {
		name string
		act  func(f *noticeFixture) error
		kind ResidentNoticeKind
		want string
	}{
		{"hide", func(f *noticeFixture) error { return f.d.ArchiveHeraRole(f.worker.ID) }, ResidentNoticeHidden, ""},
		{"nuke", func(f *noticeFixture) error { return f.d.NukeHeraRole(f.worker.ID) }, ResidentNoticeNuked, ""},
		{"rename", func(f *noticeFixture) error { return f.d.RenameHeraRole(f.worker.ID, "beta") }, ResidentNoticeRenamed, "alpha → beta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newNoticeFixture(t)
			testutil.NoError(t, tc.act(f))
			ns := f.pending(t)
			testutil.Equal(t, len(ns), 1)
			testutil.Equal(t, ns[0].Kind, tc.kind)
			testutil.Equal(t, ns[0].WorkerRoleID, f.worker.ID)
			if tc.want != "" {
				testutil.Equal(t, ns[0].Detail, tc.want)
			}
		})
	}
}

func TestResidentNotice_ResidentsOwnNukeIsNotEchoed(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.NukeHeraRoleAs(f.worker.ID, HeraActorResident))
	testutil.Equal(t, len(f.pending(t)), 0)
}

func TestResidentNotice_StatusStepsAndUnhideAreSilent(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.ArchiveHeraRoleAs(f.worker.ID, HeraActorResident)) // baseline: no notice
	testutil.NoError(t, f.d.UnarchiveHeraRole(f.worker.ID))
	testutil.NoError(t, f.d.UpsertHeraRoleStatus(f.worker.ID, HeraRoleStatusValue("working")))
	testutil.Equal(t, len(f.pending(t)), 0)
}

func TestResidentNotice_IdempotentRepeatsAndFailuresEnqueueNothing(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID))
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID)) // already hidden
	testutil.Equal(t, len(f.pending(t)), 1)

	// Renaming to the same name or onto a taken name enqueues nothing.
	testutil.NoError(t, f.d.RenameHeraRole(f.worker.ID, "alpha"))
	testutil.Equal(t, len(f.pending(t)), 1)
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID)) // already nuked
	testutil.Equal(t, len(f.pending(t)), 2)
}

func TestResidentNotice_HideSurvivesTaskMetaWipeAndUnhide(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.SetMeta(f.task, "hera", HeraMetaKeyResidentWorker, "true"))
	// SetArchived wipes task_meta (incl. the resident_worker marker); membership
	// and the notice path must not depend on it.
	testutil.NoError(t, f.d.SetArchived(f.task, true))
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID))
	testutil.Equal(t, len(f.pending(t)), 1)
	testutil.NoError(t, f.d.SetArchived(f.task, false))
	testutil.NoError(t, f.d.UnarchiveHeraRole(f.worker.ID))
	// Still a resident worker by live binding after the round trip...
	yes, err := f.d.IsResidentWorker(f.task)
	testutil.NoError(t, err)
	testutil.True(t, yes)
	// ...so a second human hide notifies again.
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID))
	testutil.Equal(t, len(f.pending(t)), 2)
}

func TestResidentNotice_OrdinaryWorkersAndCoordinatorsAreNotNoticed(t *testing.T) {
	f := newNoticeFixture(t)
	other, err := f.d.CreateHeraOrchestrator("proj", "")
	testutil.NoError(t, err)
	r, err := f.d.CreateHeraRole(CreateHeraRoleInput{OrchestratorID: other.ID, Name: "w", Kind: HeraKindWorker})
	testutil.NoError(t, err)
	testutil.NoError(t, f.d.ArchiveHeraRole(r.ID))
	testutil.NoError(t, f.d.RenameHeraRole(r.ID, "w2"))
	testutil.NoError(t, f.d.NukeHeraRole(r.ID))
	testutil.Equal(t, len(f.pending(t)), 0)
	// The resident coordinator row is protected by residentHeraGuard and never noticed.
	roles, err := f.d.ListHeraRoles(f.orch.ID, true)
	testutil.NoError(t, err)
	for _, ro := range roles {
		if ro.Kind == HeraKindCoordinator {
			testutil.True(t, f.d.ArchiveHeraRole(ro.ID) != nil)
		}
	}
	testutil.Equal(t, len(f.pending(t)), 0)
}

func TestResidentNotice_TaskListDeleteIsANuke(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.Delete(f.task))
	ns := f.pending(t)
	testutil.Equal(t, len(ns), 1)
	testutil.Equal(t, ns[0].Kind, ResidentNoticeNuked)
	testutil.True(t, strings.Contains(ns[0].Detail, "Tasks list"))
}

func TestResidentNotice_LegacyClaimedRowIsStillPending(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
	// A row left claimed by the retired claim protocol (daemon crashed mid-send).
	_, err := f.d.conn.Exec(`UPDATE resident_notices SET claimed_at=?`, formatTime(time.Now()))
	testutil.NoError(t, err)
	testutil.Equal(t, len(f.pending(t)), 1)
}

func (f *noticeFixture) coordID(t *testing.T) int64 {
	t.Helper()
	roles, err := f.d.ListHeraRoles(f.orch.ID, true)
	testutil.NoError(t, err)
	for _, r := range roles {
		if r.Kind == HeraKindCoordinator {
			return r.ID
		}
	}
	t.Fatal("no coordinator")
	return 0
}

func TestDeliverResidentNotice_AtomicAndIdempotent(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
	n := f.pending(t)[0]
	coord := f.coordID(t)

	// A failing insert (tldr too long) rolls the delete back: the row stays.
	_, err := f.d.DeliverResidentNotice(n.ID, f.worker.ID, coord, "b", strings.Repeat("x", HeraMaxTldrLen+1))
	testutil.ErrorIs(t, err, ErrHeraMessageTldrTooLong)
	testutil.Equal(t, len(f.pending(t)), 1)

	msg, err := f.d.DeliverResidentNotice(n.ID, f.worker.ID, coord, "b", "t")
	testutil.NoError(t, err)
	testutil.True(t, msg != nil)
	testutil.Equal(t, len(f.pending(t)), 0)
	// Second call: row already gone -> no message, no duplicate.
	msg, err = f.d.DeliverResidentNotice(n.ID, f.worker.ID, coord, "b", "t")
	testutil.NoError(t, err)
	testutil.True(t, msg == nil)
	inbox, err := f.d.HeraInbox(coord)
	testutil.NoError(t, err)
	testutil.Equal(t, len(inbox), 1)
}

func TestResidentNotice_NoticeInsertFailureRollsBackMutation(t *testing.T) {
	cases := []struct {
		name string
		act  func(f *noticeFixture) error
		done func(r *HeraRole) bool
	}{
		{"hide", func(f *noticeFixture) error { return f.d.ArchiveHeraRole(f.worker.ID) }, func(r *HeraRole) bool { return r.ArchivedAt != nil }},
		{"nuke", func(f *noticeFixture) error { return f.d.NukeHeraRole(f.worker.ID) }, func(r *HeraRole) bool { return r.NukedAt != nil }},
		{"rename", func(f *noticeFixture) error { return f.d.RenameHeraRole(f.worker.ID, "beta") }, func(r *HeraRole) bool { return r.Name != "alpha" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newNoticeFixture(t)
			_, err := f.d.conn.Exec(`CREATE TRIGGER fail_notice BEFORE INSERT ON resident_notices BEGIN SELECT RAISE(ABORT, 'boom'); END`)
			testutil.NoError(t, err)
			testutil.True(t, tc.act(f) != nil)
			r, err := f.d.HeraRole(f.worker.ID)
			testutil.NoError(t, err)
			testutil.False(t, tc.done(r)) // mutation rolled back with the notice
			// Once the failure clears, the same action succeeds and queues one notice.
			_, err = f.d.conn.Exec(`DROP TRIGGER fail_notice`)
			testutil.NoError(t, err)
			testutil.NoError(t, tc.act(f))
			testutil.Equal(t, len(f.pending(t)), 1)
		})
	}
}

func TestResidentNotice_RepeatedActionsQueueOnce(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID))
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID))
	testutil.Equal(t, len(f.pending(t)), 1)
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
	testutil.Equal(t, len(f.pending(t)), 2)                       // one hide + one nuke
	testutil.NoError(t, f.d.RenameHeraRole(f.worker.ID, "alpha")) // same name: no change
	testutil.Equal(t, len(f.pending(t)), 2)
}

func TestResidentNotice_ConcurrentHidesAcrossHandlesQueueOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("two-handle concurrency")
	}
	path := t.TempDir() + "/data.sql"
	d1, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d1.Close() })
	d2, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d2.Close() })
	rt, _, err := d1.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d1.EnsureResidentOrchestrator(rt.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d1.Add(&model.Task{ID: "wt", Name: "alpha", Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))
	w, _, err := d1.CreateResidentWorkerRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "alpha", Kind: HeraKindWorker, ArgusProject: "p"}, "wt", t.TempDir())
	testutil.NoError(t, err)

	// Deterministic interleave: d2 holds the write lock (BEGIN IMMEDIATE) while
	// d1's hide starts, then commits a write. With the write-lock-first no-op
	// UPDATE in heraRoleMutation, d1 blocks on the lock before it reads and then
	// proceeds; without it d1 reads a snapshot first and its later write fails
	// with SQLITE_BUSY_SNAPSHOT once d2 commits in between.
	ctx := context.Background()
	holder, err := d2.conn.Conn(ctx)
	testutil.NoError(t, err)
	_, err = holder.ExecContext(ctx, `BEGIN IMMEDIATE`)
	testutil.NoError(t, err)
	_, err = holder.ExecContext(ctx, `UPDATE hera_roles SET id=id WHERE id=?`, w.ID)
	testutil.NoError(t, err)

	errs := make(chan error, 1)
	go func() { errs <- d1.ArchiveHeraRole(w.ID) }()
	// Give d1 time to begin its transaction and reach the lock (or, on the
	// mutated code, to finish its snapshot read). The pause only widens the
	// window; correct code passes at any timing.
	time.Sleep(200 * time.Millisecond)
	_, err = holder.ExecContext(ctx, `COMMIT`)
	testutil.NoError(t, err)
	testutil.NoError(t, holder.Close())
	testutil.NoError(t, <-errs)

	// A second hide from the other handle is a no-op: still exactly one notice.
	testutil.NoError(t, d2.ArchiveHeraRole(w.ID))
	ns, err := d1.PendingResidentNotices(10)
	testutil.NoError(t, err)
	testutil.Equal(t, len(ns), 1)
}
