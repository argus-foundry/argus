package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
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
	fyi, fyi2     *HeraBoardItem
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
	f.fyi2, err = d.PostHeraBoardItem(f.cc.ID, "fyi", "second", "body2")
	testutil.NoError(t, err)
	// Exclusions a real done-done sub-coordinator carries: a resolved decision,
	// an archived child worker, a cancelled planned child, and a DONE dependent.
	dec, err := d.PostHeraBoardItem(f.cc.ID, "decision", "settled", "b")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ResolveHeraBoardItem(f.cc.ID, dec.ID, "agent", "ok"))
	kt := &model.Task{Name: "kid-done", Status: model.StatusComplete, Project: "p"}
	testutil.NoError(t, d.Add(kt))
	kid, _, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.child.ID, Name: "kid-done", Kind: HeraKindWorker, ArgusProject: "p"}, kt.ID, "/wt/kid-done")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ArchiveHeraRole(kid.ID))
	planned, err := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: f.child.ID, Name: "kid-planned", Kind: HeraKindWorker, ArgusProject: "p", Prompt: "later"})
	testutil.NoError(t, err)
	testutil.NoError(t, d.CancelHeraPlannedNode(planned.ID))
	dep, err := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: f.parent.ID, Name: "dep-done", Kind: HeraKindWorker, ArgusProject: "p", Prompt: "later"})
	testutil.NoError(t, err)
	testutil.NoError(t, d.AddHeraBlock(dep.ID, f.sub.ID))
	testutil.NoError(t, d.UpsertHeraRoleStatus(dep.ID, HeraStatusDone))
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
	testutil.Equal(t, len(left), 2)
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
	// BOTH open fyi items moved, each carrying its idempotence marker.
	up, _ := f.d.ListHeraBoardItems(f.pc.ID, false)
	testutil.Equal(t, len(up), 2)
	for i, it := range []*HeraBoardItem{f.fyi, f.fyi2} {
		testutil.Equal(t, up[i].Kind, "fyi")
		testutil.Equal(t, strings.HasPrefix(up[i].Subject, "[sub] "), true)
		testutil.Contains(t, up[i].Body, "— moved from sub fyi #"+itoa(it.ID))
	}
	left, _ := f.d.ListHeraBoardItems(f.cc.ID, false)
	testutil.Equal(t, len(left), 0)
	// The child coordinator hat is ended with the restorable "accepted" reason.
	live, _ := f.d.ListHeraLiveBindingsByTask(f.task.ID)
	testutil.Equal(t, len(live), 1)
	var reason string
	testutil.NoError(t, f.d.conn.QueryRow(`SELECT end_reason FROM hera_bindings WHERE role_id=?`, f.cc.ID).Scan(&reason))
	testutil.Equal(t, reason, HeraEndReasonAccepted)
}

// Every write step failing (child-hat end, child orchestrator archive, task
// archive, role hide, fyi push) must roll the WHOLE unit back. The recorded
// step order proves the EARLIER writes really ran before the failure (so the
// rollback is what undid them), and that the failure is the injected one, not
// a refusal before any write.
func TestArchiveHeraUnit_AnyStepFailureRollsBackEverything(t *testing.T) {
	order := []string{"merge_notices", "fyi", "end_hat", "child_orch", "task", "role"}
	for i, step := range order {
		t.Run(step, func(t *testing.T) {
			f := newUnitFixture(t)
			testutil.NoError(t, f.d.SetMeta(f.task.ID, "x", "k", "v"))
			var seen []string
			archiveUnitStepHook = func(s string) error {
				seen = append(seen, s)
				if s == step {
					return errors.New("injected")
				}
				return nil
			}
			t.Cleanup(func() { archiveUnitStepHook = nil })
			_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("expected the injected failure, got %v", err)
			}
			var refused *ArchiveRefusedError
			testutil.Equal(t, errors.As(err, &refused), false)
			testutil.DeepEqual(t, seen, order[:i+1])
			f.assertUntouched(t)
			// task_meta wiped by setArchivedTx must survive a later rollback.
			var v string
			testutil.NoError(t, f.d.conn.QueryRow(`SELECT value FROM task_meta WHERE task_id=? AND namespace='x'`, f.task.ID).Scan(&v))
			testutil.Equal(t, v, "v")
		})
	}
}

// PreCommit is the LAST step (after every write) and its refusal rolls back.
func TestArchiveHeraUnit_PreCommitRunsLastAndRefusalRollsBack(t *testing.T) {
	f := newUnitFixture(t)
	var seen []string
	archiveUnitStepHook = func(s string) error { seen = append(seen, s); return nil }
	t.Cleanup(func() { archiveUnitStepHook = nil })
	_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID, PreCommit: func(string) error {
		seen = append(seen, "precommit")
		return &ArchiveRefusedError{Reason: "session busy"}
	}})
	var refused *ArchiveRefusedError
	testutil.Equal(t, errors.As(err, &refused), true)
	testutil.DeepEqual(t, seen, []string{"merge_notices", "fyi", "end_hat", "child_orch", "task", "role", "precommit"})
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
	testutil.Equal(t, len(up), 3) // fixture's 2 fyi on the (unbound) original coordinator + 1 on coord2
	// The link-only child's coordinator lives on a DIFFERENT task: its binding must be ended too.
	var ended int
	testutil.NoError(t, f.d.conn.QueryRow(`SELECT COUNT(*) FROM hera_bindings WHERE role_id=? AND ended_at IS NOT NULL`, cc2.ID).Scan(&ended))
	testutil.Equal(t, ended, 1)
}

// Marker dedupe must be an exact suffix match: role names containing LIKE
// wildcards, or a different role's marker for the same item id, must not
// suppress the repost (which would silently drop the fyi).
func TestArchiveHeraUnit_FyiDedupeIsExactNotLike(t *testing.T) {
	f := newUnitFixture(t)
	// A parent-board item whose body ends with ANOTHER role's marker for the same id.
	_, err := f.d.PostHeraBoardItem(f.pc.ID, "fyi", "other", "x\n\n— moved from su_ fyi #"+itoa(f.fyi.ID))
	testutil.NoError(t, err)
	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
	testutil.NoError(t, err)
	up, _ := f.d.ListHeraBoardItems(f.pc.ID, false)
	testutil.Equal(t, len(up), 3) // foreign item + both re-posted fyi items
	left, _ := f.d.ListHeraBoardItems(f.cc.ID, false)
	testutil.Equal(t, len(left), 0)
}

func TestArchiveHeraUnit_OpenDecisionOnArchivedChildCoordinatorDoesNotBlock(t *testing.T) {
	f := newUnitFixture(t)
	d2, err := f.d.PostHeraBoardItem(f.cc.ID, "decision", "stale", "b")
	testutil.NoError(t, err)
	_ = d2
	// Archive the child coordinator ROLE (a stale decision left behind): SQL and Go agree it's ignored.
	testutil.NoError(t, f.d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE hera_roles SET archived_at=? WHERE id=?`, formatTime(time.Now()), f.cc.ID)
		return err
	}))
	_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
	testutil.NoError(t, err)
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

// recSink records every emitted event type and, on each, whether the database
// lock is free (i.e. we are past commit).
type recSink struct {
	types []string
	ops   []string
}

func (r *recSink) Emit(e model.Event) {
	r.types = append(r.types, e.Type)
	if e.Type == model.EventTypeHeraBoardChanged {
		var p struct {
			Operation string `json:"operation"`
		}
		b, _ := json.Marshal(e.Payload)
		_ = json.Unmarshal(b, &p)
		r.ops = append(r.ops, p.Operation)
	}
}

func TestArchiveHeraUnit_EventsOnlyAfterCommit(t *testing.T) {
	t.Run("success emits archived + board events", func(t *testing.T) {
		f := newUnitFixture(t)
		rec := &recSink{}
		prev := events.SetSink(rec)
		t.Cleanup(func() { events.SetSink(prev) })
		_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
		testutil.NoError(t, err)
		testutil.Contains(t, strings.Join(rec.types, ","), model.EventTypeTaskArchived)
		testutil.Equal(t, strings.Count(strings.Join(rec.ops, ","), "posted"), 2)
		testutil.Equal(t, strings.Count(strings.Join(rec.ops, ","), "resolved"), 2)
	})
	t.Run("rolled-back unit emits nothing", func(t *testing.T) {
		f := newUnitFixture(t)
		rec := &recSink{}
		prev := events.SetSink(rec)
		t.Cleanup(func() { events.SetSink(prev) })
		archiveUnitStepHook = func(s string) error {
			if s == "role" {
				return errors.New("injected")
			}
			return nil
		}
		t.Cleanup(func() { archiveUnitStepHook = nil })
		_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
		if err == nil {
			t.Fatal("expected failure")
		}
		testutil.Equal(t, len(rec.types), 0)
	})
}

// Positive exclusions inside clauses: these must NOT refuse.
func TestArchiveHeraUnit_ExclusionsDoNotRefuse(t *testing.T) {
	t.Run("sent mail to an archived recipient", func(t *testing.T) {
		f := newUnitFixture(t)
		other := &model.Task{Name: "o", Status: model.StatusInProgress, Project: "p"}
		testutil.NoError(t, f.d.Add(other))
		r, _, err := f.d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: f.parent.ID, Name: "gone", Kind: HeraKindWorker, ArgusProject: "p"}, other.ID, "/wt/o")
		testutil.NoError(t, err)
		_, err = f.d.SendHeraMessage(f.sub.ID, r.ID, "x", "t", nil)
		testutil.NoError(t, err)
		testutil.NoError(t, f.d.ArchiveHeraRole(r.ID))
		_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
		testutil.NoError(t, err)
	})
}

func TestArchiveHeraUnit_ResidentRefusals(t *testing.T) {
	t.Run("resident-kind task", func(t *testing.T) {
		f := newUnitFixture(t)
		testutil.NoError(t, f.d.conn2Exec(`UPDATE tasks SET kind='resident' WHERE id=?`, f.task.ID))
		_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
		var refused *ArchiveRefusedError
		testutil.Equal(t, errors.As(err, &refused), true)
		testutil.Contains(t, refused.Reason, "resident")
	})
	t.Run("child orchestrator is the standing team", func(t *testing.T) {
		f := newUnitFixture(t)
		testutil.NoError(t, f.d.conn2Exec(`UPDATE hera_orchestrators SET name=? WHERE id=?`, ResidentOrchestratorName, f.child.ID))
		_, err := f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
		var refused *ArchiveRefusedError
		testutil.Equal(t, errors.As(err, &refused), true)
		testutil.Contains(t, refused.Reason, "standing team")
	})
}

func (d *DB) conn2Exec(q string, args ...any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(q, args...)
	return err
}
