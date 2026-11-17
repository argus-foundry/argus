package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// handoffFixture: one orchestrator, a coordinator, and a worker whose task is
// in_review (a PR is open; the batch owns the work).
type handoffFixture struct {
	d      *DB
	orch   *HeraOrchestrator
	coord  *HeraRole
	worker *HeraRole
	task   *model.Task
}

func newHandoffFixture(t *testing.T) *handoffFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	f := &handoffFixture{d: d}
	f.orch, err = d.CreateHeraOrchestrator("batch", "master")
	testutil.NoError(t, err)
	ct := &model.Task{Name: "coord", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(ct))
	f.coord, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.orch.ID, Name: "coord", Kind: HeraKindCoordinator, ArgusProject: "p"}, ct.ID, "/wt/c")
	testutil.NoError(t, err)
	f.task = &model.Task{Name: "w1", Status: model.StatusInProgress, Project: "p", Worktree: "/wt/w1", Branch: "b1"}
	testutil.NoError(t, d.Add(f.task))
	f.worker, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.orch.ID, Name: "w1", Kind: HeraKindWorker, ArgusProject: "p"}, f.task.ID, f.task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetStatus(f.task.ID, model.StatusInReview))
	return f
}

func archivedOf(t *testing.T, f *handoffFixture) (task, role bool) {
	t.Helper()
	g, err := f.d.Get(f.task.ID)
	testutil.NoError(t, err)
	r, err := f.d.HeraRole(f.worker.ID)
	testutil.NoError(t, err)
	return g.Archived, r.ArchivedAt != nil
}

func TestArchiveHeraUnit_KindStatusRules(t *testing.T) {
	cases := []struct {
		name   string
		kind   ArchiveKind
		status model.Status
		reason string
		ok     bool
		want   string
	}{
		{"merged needs complete", ArchiveKindMerged, model.StatusInReview, "", false, "not complete"},
		{"merged complete ok", ArchiveKindMerged, model.StatusComplete, "", true, ""},
		{"handoff in_review ok", ArchiveKindHandedOff, model.StatusInReview, "", true, ""},
		{"handoff complete ok", ArchiveKindHandedOff, model.StatusComplete, "", true, ""},
		{"handoff in_progress refused", ArchiveKindHandedOff, model.StatusInProgress, "", false, "not in_review or complete"},
		{"forced ignores status", ArchiveKindForced, model.StatusInProgress, "batch owns it", true, ""},
		{"forced needs reason", ArchiveKindForced, model.StatusInReview, "  ", false, "non-empty reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newHandoffFixture(t)
			testutil.NoError(t, f.d.SetStatus(f.task.ID, tc.status))
			_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: tc.kind, Reason: tc.reason, PRURL: "https://x/pr/1", TipSHA: "abc"})
			ta, ra := archivedOf(t, f)
			if tc.ok {
				testutil.NoError(t, err)
				testutil.Equal(t, ta, true)
				testutil.Equal(t, ra, true)
				recs, rerr := f.d.ArchiveRecordsForRole(f.worker.ID)
				testutil.NoError(t, rerr)
				testutil.Equal(t, len(recs), 1)
				testutil.Equal(t, recs[0].Kind, tc.kind)
				testutil.Equal(t, recs[0].Reason, strings.TrimSpace(tc.reason))
				testutil.Equal(t, recs[0].TipSHA, "abc")
				return
			}
			var refused *ArchiveRefusedError
			testutil.Equal(t, errors.As(err, &refused), true)
			testutil.Contains(t, refused.Reason, tc.want)
			testutil.Equal(t, ta, false)
			testutil.Equal(t, ra, false)
			recs, _ := f.d.ArchiveRecordsForRole(f.worker.ID)
			testutil.Equal(t, len(recs), 0)
		})
	}
}

// Hard refusals survive force; soft clauses (blocked, outbox, dependents) do not.
func TestArchiveHeraUnit_ForcedHardAndSoftClauses(t *testing.T) {
	mutations := map[string]struct {
		mutate func(t *testing.T, f *handoffFixture)
		hard   bool
		want   string
	}{
		"pinned role": {func(t *testing.T, f *handoffFixture) { testutil.NoError(t, f.d.PinHeraRole(f.worker.ID)) }, true, "pinned"},
		"pinned task": {func(t *testing.T, f *handoffFixture) {
			tk, _ := f.d.Get(f.task.ID)
			tk.Pinned = true
			testutil.NoError(t, f.d.Update(tk))
		}, true, "pinned"},
		"resident marker": {func(t *testing.T, f *handoffFixture) {
			testutil.NoError(t, f.d.SetMeta(f.task.ID, "hera", "resident_worker", "true"))
		}, true, "standing-team"},
		"other live role": {func(t *testing.T, f *handoffFixture) {
			o, _ := f.d.CreateHeraOrchestrator("o2", "")
			_, _, err := f.d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "x", Kind: HeraKindFreelance, ArgusProject: "p"}, f.task.ID, f.task.Worktree)
			testutil.NoError(t, err)
		}, true, "another live role"},
		"blocked": {func(t *testing.T, f *handoffFixture) {
			testutil.NoError(t, f.d.UpsertHeraRoleStatus(f.worker.ID, HeraStatusBlocked))
		}, false, "blocked"},
		"unread outbox": {func(t *testing.T, f *handoffFixture) {
			_, err := f.d.SendHeraMessage(f.worker.ID, f.coord.ID, "q?", "question", nil)
			testutil.NoError(t, err)
		}, false, "not yet read"},
		"live dependent": {func(t *testing.T, f *handoffFixture) {
			dep, err := f.d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: f.orch.ID, Name: "dep", Kind: HeraKindWorker, ArgusProject: "p", Prompt: "later"})
			testutil.NoError(t, err)
			testutil.NoError(t, f.d.AddHeraBlock(dep.ID, f.worker.ID))
		}, false, "depends"},
	}
	for name, m := range mutations {
		t.Run(name, func(t *testing.T) {
			f := newHandoffFixture(t)
			m.mutate(t, f)
			// Handoff (non-forced) refuses every one.
			_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindHandedOff})
			var refused *ArchiveRefusedError
			testutil.Equal(t, errors.As(err, &refused), true)
			testutil.Contains(t, refused.Reason, m.want)
			// Forced refuses only the hard ones.
			_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindForced, Reason: "because"})
			if m.hard {
				testutil.Equal(t, errors.As(err, &refused), true)
				testutil.Contains(t, refused.Reason, m.want)
				ta, _ := archivedOf(t, f)
				testutil.Equal(t, ta, false)
			} else {
				testutil.NoError(t, err)
			}
		})
	}
}

func TestArchiveHeraUnit_CoordinatorRefusedEvenForced(t *testing.T) {
	f := newHandoffFixture(t)
	_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.coord.ID, Kind: ArchiveKindForced, Reason: "x"})
	var refused *ArchiveRefusedError
	testutil.Equal(t, errors.As(err, &refused), true)
	testutil.Contains(t, refused.Reason, "coordinator")
}

func TestArchiveHeraUnit_UnreadMailForwardedAsOneDigest(t *testing.T) {
	f := newHandoffFixture(t)
	m1, err := f.d.SendHeraMessage(f.coord.ID, f.worker.ID, "please rebase\nsecond line stays out", "rebase", nil)
	testutil.NoError(t, err)
	_, err = f.d.SendHeraMessage(f.coord.ID, f.worker.ID, "also fix lint", "lint", nil)
	testutil.NoError(t, err)
	var got *HeraMessage
	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindHandedOff, PostCommit: func(m *HeraMessage) { got = m }})
	testutil.NoError(t, err)
	testutil.Equal(t, got != nil, true)
	in, err := f.d.HeraInbox(f.coord.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(in), 1)
	testutil.Equal(t, in[0].FromRoleID, f.worker.ID)
	testutil.Contains(t, in[0].Body, "#"+itoa(m1.ID)+" from coord: please rebase")
	testutil.Contains(t, in[0].Body, "also fix lint")
	testutil.Equal(t, strings.Contains(in[0].Body, "second line"), false)
	// Originals are read; nothing unread left for the archived role.
	n, err := f.d.HeraArchiveUnread(f.worker.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, n, 0)
}

func TestArchiveHeraUnit_ForcedReasonInDigest(t *testing.T) {
	f := newHandoffFixture(t)
	_, err := f.d.SendHeraMessage(f.coord.ID, f.worker.ID, "ping", "ping", nil)
	testutil.NoError(t, err)
	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindForced, Reason: "batch owns it"})
	testutil.NoError(t, err)
	in, _ := f.d.HeraInbox(f.coord.ID)
	testutil.Equal(t, len(in), 1)
	testutil.Contains(t, in[0].Body, "Forced archive reason: batch owns it")
}

func TestArchiveHeraUnit_DigestFailureRollsBackAndKeepsMailUnread(t *testing.T) {
	f := newHandoffFixture(t)
	_, err := f.d.SendHeraMessage(f.coord.ID, f.worker.ID, "hi", "hi", nil)
	testutil.NoError(t, err)
	// Stand in for a full coordinator inbox / rate limit: any new message insert fails.
	_, err = f.d.conn.Exec(`CREATE TRIGGER reject_digest BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT, 'inbox full'); END`)
	testutil.NoError(t, err)
	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindHandedOff})
	var refused *ArchiveRefusedError
	testutil.Equal(t, errors.As(err, &refused), true)
	testutil.Contains(t, refused.Reason, "forward")
	n, _ := f.d.HeraArchiveUnread(f.worker.ID)
	testutil.Equal(t, n, 1)
	ta, ra := archivedOf(t, f)
	testutil.Equal(t, ta, false)
	testutil.Equal(t, ra, false)
}

func TestArchiveHeraUnit_ArchivesTheTaskInTheSameTx(t *testing.T) {
	f := newHandoffFixture(t)
	testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusComplete))
	_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID})
	testutil.NoError(t, err)
	ta, ra := archivedOf(t, f)
	testutil.Equal(t, ta, true)
	testutil.Equal(t, ra, true)
}

func TestHeraRoleLiveGuard_BackstopOnSetArchivedAndUpdate(t *testing.T) {
	f := newHandoffFixture(t)
	testutil.ErrorIs(t, f.d.SetArchived(f.task.ID, true), ErrHeraRoleLive)
	tk, _ := f.d.Get(f.task.ID)
	tk.SetArchived(true)
	testutil.ErrorIs(t, f.d.Update(tk), ErrHeraRoleLive)
	g, _ := f.d.Get(f.task.ID)
	testutil.Equal(t, g.Archived, false)
	// A hidden role, a coordinator-only task, and an unbound task all pass.
	testutil.NoError(t, f.d.ArchiveHeraRole(f.worker.ID))
	testutil.NoError(t, f.d.SetArchived(f.task.ID, true))
	plain := &model.Task{Name: "plain", Status: model.StatusInProgress}
	testutil.NoError(t, f.d.Add(plain))
	testutil.NoError(t, f.d.SetArchived(plain.ID, true))
}

// Nuke/reclaim/prune paths: nuke ends the binding first, and a nuked role never trips the guard.
func TestHeraRoleLiveGuard_NukeReclaimPrunePathsUnaffected(t *testing.T) {
	t.Run("nuked role then archive", func(t *testing.T) {
		f := newHandoffFixture(t)
		testutil.NoError(t, f.d.NukeHeraRole(f.worker.ID))
		testutil.NoError(t, f.d.SetArchived(f.task.ID, true))
	})
	t.Run("ended binding then archive", func(t *testing.T) {
		f := newHandoffFixture(t)
		_, err := f.d.EndHeraBindingsForTask(f.task.ID, heraEndReasonTaskDeleted)
		testutil.NoError(t, err)
		testutil.NoError(t, f.d.SetArchived(f.task.ID, true))
	})
	t.Run("delete still works with a live binding", func(t *testing.T) {
		f := newHandoffFixture(t)
		testutil.NoError(t, f.d.Delete(f.task.ID))
	})
}

func archiveHandedOff(t *testing.T, f *handoffFixture) {
	t.Helper()
	_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindHandedOff, PRURL: "https://x/pr/1"})
	testutil.NoError(t, err)
}

func TestOpenHandoffRecords_OnlyUnsettledHandedOff(t *testing.T) {
	f := newHandoffFixture(t)
	m, _ := f.d.OpenHandoffRecords()
	testutil.Equal(t, len(m), 0)
	archiveHandedOff(t, f)
	m, err := f.d.OpenHandoffRecords()
	testutil.NoError(t, err)
	testutil.Equal(t, m[f.task.ID].PRURL, "https://x/pr/1")
	_, err = f.d.conn.Exec(`UPDATE hera_archive_records SET settled_at='2026-01-01T00:00:00Z'`)
	testutil.NoError(t, err)
	m, _ = f.d.OpenHandoffRecords()
	testutil.Equal(t, len(m), 0)

	g := newHandoffFixture(t)
	_, err = g.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: g.worker.ID, Kind: ArchiveKindForced, Reason: "r"})
	testutil.NoError(t, err)
	m, _ = g.d.OpenHandoffRecords()
	testutil.Equal(t, len(m), 0)
}

func TestSettleHandoff_Merged(t *testing.T) {
	f := newHandoffFixture(t)
	archiveHandedOff(t, f)
	ok, err := f.d.SettleHandoff(f.task.ID, HandoffMerged, "https://x/pr/1")
	testutil.NoError(t, err)
	testutil.Equal(t, ok, true)
	g, _ := f.d.Get(f.task.ID)
	testutil.Equal(t, g.Status, model.StatusComplete)
	testutil.Equal(t, g.Archived, true)
	items, _ := f.d.ListHeraBoardItems(f.coord.ID, false)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "fyi")
	recs, _ := f.d.ArchiveRecordsForRole(f.worker.ID)
	testutil.Equal(t, recs[0].SettleNote, "merged")
	testutil.Equal(t, recs[0].SettledAt.IsZero(), false)
	// Idempotent: a re-poll writes nothing.
	ok, err = f.d.SettleHandoff(f.task.ID, HandoffMerged, "https://x/pr/1")
	testutil.NoError(t, err)
	testutil.Equal(t, ok, false)
	items, _ = f.d.ListHeraBoardItems(f.coord.ID, false)
	testutil.Equal(t, len(items), 1)
}

func TestSettleHandoff_ClosedUnmergedRevivesAndPostsDecision(t *testing.T) {
	f := newHandoffFixture(t)
	testutil.NoError(t, f.d.UpsertHeraRoleStatus(f.worker.ID, HeraStatusDone))
	archiveHandedOff(t, f)
	ok, err := f.d.SettleHandoff(f.task.ID, HandoffClosed, "https://x/pr/1")
	testutil.NoError(t, err)
	testutil.Equal(t, ok, true)
	g, _ := f.d.Get(f.task.ID)
	testutil.Equal(t, g.Archived, false)
	testutil.Equal(t, g.Status, model.StatusInProgress)
	r, _ := f.d.HeraRole(f.worker.ID)
	testutil.Nil(t, r.ArchivedAt)
	rs, _ := f.d.HeraRoleStatusFor(f.worker.ID)
	testutil.Equal(t, rs.Status, HeraStatusWorking)
	items, _ := f.d.ListHeraBoardItems(f.coord.ID, false)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "decision")
	testutil.Contains(t, items[0].Body, "WITHOUT merging")
	recs, _ := f.d.ArchiveRecordsForRole(f.worker.ID)
	testutil.Equal(t, recs[0].SettleNote, "closed unmerged")
}

func TestSettleHandoff_ReviveFailureStillPostsBoardItem(t *testing.T) {
	f := newHandoffFixture(t)
	archiveHandedOff(t, f)
	// Make the un-archive fail: a trigger rejects un-hiding the role.
	_, err := f.d.conn.Exec(`CREATE TRIGGER reject_unhide BEFORE UPDATE OF archived_at ON hera_roles WHEN NEW.archived_at IS NULL BEGIN SELECT RAISE(ABORT, 'unhide failed'); END`)
	testutil.NoError(t, err)
	ok, err := f.d.SettleHandoff(f.task.ID, HandoffClosed, "https://x/pr/1")
	testutil.NoError(t, err)
	testutil.Equal(t, ok, true)
	ta, ra := archivedOf(t, f)
	testutil.Equal(t, ta, true) // rolled back: still archived
	testutil.Equal(t, ra, true)
	items, _ := f.d.ListHeraBoardItems(f.coord.ID, false)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "decision")
	testutil.Contains(t, items[0].Body, "still archived")
	recs, _ := f.d.ArchiveRecordsForRole(f.worker.ID)
	testutil.Contains(t, recs[0].SettleNote, "un-archive failed")
}

func TestSettleHandoff_NoRecordOrForcedIsNoop(t *testing.T) {
	f := newHandoffFixture(t)
	ok, err := f.d.SettleHandoff(f.task.ID, HandoffClosed, "u")
	testutil.NoError(t, err)
	testutil.Equal(t, ok, false)
	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.worker.ID, Kind: ArchiveKindForced, Reason: "r"})
	testutil.NoError(t, err)
	ok, _ = f.d.SettleHandoff(f.task.ID, HandoffClosed, "u")
	testutil.Equal(t, ok, false)
	ta, _ := archivedOf(t, f)
	testutil.Equal(t, ta, true)
}

func TestUnarchiveHeraUnit(t *testing.T) {
	t.Run("brings a handed-off role back and settles the record", func(t *testing.T) {
		f := newHandoffFixture(t)
		archiveHandedOff(t, f)
		tid, err := f.d.UnarchiveHeraUnit(UnarchiveUnitInput{RoleID: f.worker.ID, Note: "handed back"})
		testutil.NoError(t, err)
		testutil.Equal(t, tid, f.task.ID)
		ta, ra := archivedOf(t, f)
		testutil.Equal(t, ta, false)
		testutil.Equal(t, ra, false)
		g, _ := f.d.Get(f.task.ID)
		testutil.Equal(t, g.Status, model.StatusInProgress)
		recs, _ := f.d.ArchiveRecordsForRole(f.worker.ID)
		testutil.Equal(t, recs[0].SettleNote, "handed back")
		m, _ := f.d.OpenHandoffRecords()
		testutil.Equal(t, len(m), 0)
	})
	t.Run("refuses a role that is not archived", func(t *testing.T) {
		f := newHandoffFixture(t)
		_, err := f.d.UnarchiveHeraUnit(UnarchiveUnitInput{RoleID: f.worker.ID})
		var refused *ArchiveRefusedError
		testutil.Equal(t, errors.As(err, &refused), true)
		testutil.Contains(t, refused.Reason, "not archived")
	})
	t.Run("refuses a coordinator", func(t *testing.T) {
		f := newHandoffFixture(t)
		_, err := f.d.UnarchiveHeraUnit(UnarchiveUnitInput{RoleID: f.coord.ID})
		var refused *ArchiveRefusedError
		testutil.Equal(t, errors.As(err, &refused), true)
	})
}
