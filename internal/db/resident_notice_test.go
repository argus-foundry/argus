package db

import (
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
	w, _, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "alpha", Kind: HeraKindWorker, ArgusProject: "p"}, "wt", t.TempDir())
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

func TestResidentNotice_ClaimAndRelease(t *testing.T) {
	f := newNoticeFixture(t)
	testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
	ns := f.pending(t)
	ok, err := f.d.ClaimResidentNotice(ns[0].ID)
	testutil.NoError(t, err)
	testutil.True(t, ok)
	ok, err = f.d.ClaimResidentNotice(ns[0].ID)
	testutil.NoError(t, err)
	testutil.False(t, ok) // a second claim loses
	testutil.Equal(t, len(f.pending(t)), 0)
	testutil.NoError(t, f.d.ReleaseResidentNotice(ns[0].ID)) // failed send: retry later
	testutil.Equal(t, len(f.pending(t)), 1)
	ok, _ = f.d.ClaimResidentNotice(ns[0].ID)
	testutil.True(t, ok)
	testutil.NoError(t, f.d.CompleteResidentNotice(ns[0].ID))
	testutil.Equal(t, len(f.pending(t)), 0)
	testutil.NoError(t, f.d.ReleaseResidentNotice(ns[0].ID)) // gone: harmless
	testutil.Equal(t, len(f.pending(t)), 0)
}
