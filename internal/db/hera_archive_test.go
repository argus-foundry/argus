package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// unitFixture: parent orch + coordinator, a complete sub-coordinator worker role,
// and (plan-DAG shape) a child orchestrator whose coordinator hat shares the task.
type unitFixture struct {
	d             *DB
	parent, child *HeraOrchestrator
	pc, sub, cc   *HeraRole
	task          *model.Task
	fyi           *HeraBoardItem
}

func newUnitFixture(t *testing.T) *unitFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	f := &unitFixture{d: d}
	f.parent, err = d.CreateHeraOrchestrator("parent", "master")
	testutil.NoError(t, err)
	pt := &model.Task{Name: "pc", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(pt))
	f.pc, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.parent.ID, Name: "coord", Kind: HeraKindCoordinator, ArgusProject: "p"}, pt.ID, "/wt/pc")
	testutil.NoError(t, err)
	f.task = &model.Task{Name: "sub", Status: model.StatusInProgress, Project: "p", Worktree: "/wt/sub"}
	testutil.NoError(t, d.Add(f.task))
	f.sub, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.parent.ID, Name: "sub", Kind: HeraKindWorker, ArgusProject: "p"}, f.task.ID, f.task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetStatus(f.task.ID, model.StatusComplete))
	f.child, err = d.CreateHeraOrchestrator("child", "")
	testutil.NoError(t, err)
	f.cc, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.child.ID, Name: "coord", Kind: HeraKindCoordinator, ArgusProject: "p"}, f.task.ID, f.task.Worktree)
	testutil.NoError(t, err)
	f.fyi, err = d.PostHeraBoardItem(f.cc.ID, "fyi", "note", "body")
	testutil.NoError(t, err)
	return f
}

// assertUntouched proves a refused/rolled-back unit wrote NOTHING.
func (f *unitFixture) assertUntouched(t *testing.T) {
	t.Helper()
	g, _ := f.d.Get(f.task.ID)
	testutil.Equal(t, g.Archived, false)
	r, _ := f.d.HeraRole(f.sub.ID)
	testutil.Equal(t, r.ArchivedAt == nil, true)
	o, _ := f.d.HeraOrchestrator(f.child.ID)
	testutil.Equal(t, o.ArchivedAt == nil, true)
	live, _ := f.d.ListHeraLiveBindingsByTask(f.task.ID)
	testutil.Equal(t, len(live), 2) // sub worker hat + child coordinator hat
	up, _ := f.d.ListHeraBoardItems(f.pc.ID, true)
	testutil.Equal(t, len(up), 0)
	left, _ := f.d.ListHeraBoardItems(f.cc.ID, false)
	testutil.Equal(t, len(left), 1)
}

func TestArchiveHeraUnit_CommitsEverythingTogether(t *testing.T) {
	f := newUnitFixture(t)
	id, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
	testutil.NoError(t, err)
	testutil.Equal(t, id, f.task.ID)
	g, _ := f.d.Get(f.task.ID)
	testutil.Equal(t, g.Archived, true)
	r, _ := f.d.HeraRole(f.sub.ID)
	testutil.Equal(t, r.ArchivedAt != nil, true)
	o, _ := f.d.HeraOrchestrator(f.child.ID)
	testutil.Equal(t, o.ArchivedAt != nil, true)
	up, _ := f.d.ListHeraBoardItems(f.pc.ID, false)
	testutil.Equal(t, len(up), 1)
	testutil.Equal(t, up[0].Kind, "fyi")
	testutil.Equal(t, strings.HasPrefix(up[0].Subject, "[sub] "), true)
	live, _ := f.d.ListHeraLiveBindingsByTask(f.task.ID)
	testutil.Equal(t, len(live), 1)
}

// Every write step failing (child-hat end, child orchestrator archive, task
// archive, role hide, fyi push) must roll the WHOLE unit back.
func TestArchiveHeraUnit_AnyStepFailureRollsBackEverything(t *testing.T) {
	for _, step := range []string{"fyi", "end_hat", "child_orch", "task", "role"} {
		t.Run(step, func(t *testing.T) {
			f := newUnitFixture(t)
			archiveUnitStepHook = func(s string) error {
				if s == step {
					return errors.New("injected")
				}
				return nil
			}
			t.Cleanup(func() { archiveUnitStepHook = nil })
			_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
			if err == nil {
				t.Fatal("expected failure")
			}
			f.assertUntouched(t)
		})
	}
}

func TestArchiveHeraUnit_PreCommitRefusalRollsBack(t *testing.T) {
	f := newUnitFixture(t)
	_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID, PreCommit: func(string) error {
		return &ArchiveRefusedError{Reason: "session busy"}
	}})
	var refused *ArchiveRefusedError
	testutil.Equal(t, errors.As(err, &refused), true)
	f.assertUntouched(t)
}

// Every DB-side clause is re-verified INSIDE the transaction.
func TestArchiveHeraUnit_ReverifiesEveryClauseInTx(t *testing.T) {
	cases := map[string]struct {
		mutate func(t *testing.T, f *unitFixture)
		want   string
	}{
		"not complete": {func(t *testing.T, f *unitFixture) {
			testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusInProgress))
		}, "not complete"},
		"role pinned": {func(t *testing.T, f *unitFixture) { testutil.NoError(t, f.d.PinHeraRole(f.sub.ID)) }, "pinned"},
		"blocked": {func(t *testing.T, f *unitFixture) {
			testutil.NoError(t, f.d.UpsertHeraRoleStatus(f.sub.ID, HeraStatusBlocked))
		}, "blocked"},
		"resident marker": {func(t *testing.T, f *unitFixture) {
			testutil.NoError(t, f.d.SetMeta(f.task.ID, "hera", "resident_worker", "true"))
		}, "standing-team"},
		"unread inbound": {func(t *testing.T, f *unitFixture) {
			_, err := f.d.SendHeraMessage(f.pc.ID, f.sub.ID, "x", "t", nil)
			testutil.NoError(t, err)
		}, "unread hera message"},
		"unread outbound": {func(t *testing.T, f *unitFixture) {
			_, err := f.d.SendHeraMessage(f.sub.ID, f.pc.ID, "x", "t", nil)
			testutil.NoError(t, err)
		}, "not yet read"},
		"other live role": {func(t *testing.T, f *unitFixture) {
			o, _ := f.d.CreateHeraOrchestrator("o3", "")
			_, _, err := f.d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w", Kind: HeraKindFreelance, ArgusProject: "p"}, f.task.ID, f.task.Worktree)
			testutil.NoError(t, err)
		}, "another live role"},
		"live plan dependent": {func(t *testing.T, f *unitFixture) {
			dep, err := f.d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: f.parent.ID, Name: "dep", Kind: HeraKindWorker, ArgusProject: "p", Prompt: "later"})
			testutil.NoError(t, err)
			testutil.NoError(t, f.d.AddHeraBlock(dep.ID, f.sub.ID))
		}, "depends"},
		"live child role": {func(t *testing.T, f *unitFixture) {
			tk := &model.Task{Name: "kid", Status: model.StatusInProgress, Project: "p"}
			testutil.NoError(t, f.d.Add(tk))
			_, _, err := f.d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.child.ID, Name: "kid", Kind: HeraKindWorker, ArgusProject: "p"}, tk.ID, "/wt/kid")
			testutil.NoError(t, err)
		}, "kid"},
		"open decision": {func(t *testing.T, f *unitFixture) {
			_, err := f.d.PostHeraBoardItem(f.cc.ID, "decision", "pick", "b")
			testutil.NoError(t, err)
		}, "decision"},
		"pinned task": {func(t *testing.T, f *unitFixture) {
			tk, _ := f.d.Get(f.task.ID)
			tk.Pinned = true
			testutil.NoError(t, f.d.Update(tk))
		}, "pinned"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newUnitFixture(t)
			tc.mutate(t, f)
			_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
			var refused *ArchiveRefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("expected ArchiveRefusedError, got %v", err)
			}
			testutil.Contains(t, refused.Reason, tc.want)
			g, _ := f.d.Get(f.task.ID)
			testutil.Equal(t, g.Archived, false)
		})
	}
}

func TestTruncateUTF8(t *testing.T) {
	testutil.Equal(t, TruncateUTF8("abc", 10), "abc")
	testutil.Equal(t, TruncateUTF8("abc", 2), "ab")
	testutil.Equal(t, TruncateUTF8("ééé", 3), "é") // never splits a 2-byte rune
	testutil.Equal(t, TruncateUTF8("abc", 0), "")
	testutil.Equal(t, TruncateUTF8("abc", -5), "") // negative limit must not loop forever
	testutil.Equal(t, TruncateUTF8("", -1), "")
}

// hera_orch_links children (TUI adopt path): child coordinator on a DIFFERENT
// task; the unit archives the child orchestrator and re-posts its fyi items.
func TestArchiveHeraUnit_LinkedChildOrchestrator(t *testing.T) {
	f := newUnitFixture(t)
	// Rebuild the child as link-shaped: end the shared-task coordinator hat,
	// bind a fresh coordinator on another task, and add the link row.
	testutil.NoError(t, f.d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM hera_bindings WHERE role_id=?`, f.cc.ID)
		return err
	}))
	ct := &model.Task{Name: "cc2", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, f.d.Add(ct))
	cc2, _, err := f.d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.child.ID, Name: "coord2", Kind: HeraKindCoordinator, ArgusProject: "p"}, ct.ID, "/wt/cc2")
	testutil.NoError(t, err)
	_, err = f.d.PostHeraBoardItem(cc2.ID, "fyi", "linked", "b")
	testutil.NoError(t, err)
	testutil.NoError(t, f.d.CreateHeraOrchLink(f.parent.ID, f.child.ID, f.sub.ID))

	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
	testutil.NoError(t, err)
	o, _ := f.d.HeraOrchestrator(f.child.ID)
	testutil.Equal(t, o.ArchivedAt != nil, true)
	up, _ := f.d.ListHeraBoardItems(f.pc.ID, false)
	testutil.Equal(t, len(up) >= 1, true)
}
