package db

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func residentDir(t *testing.T) string { return filepath.Join(t.TempDir(), "resident") }

func TestEnsureResidentTask_CreatesSingletonRow(t *testing.T) {
	d := testDB(t)
	dir := residentDir(t)

	got, created, err := d.EnsureResidentTask(dir)
	testutil.NoError(t, err)
	testutil.Equal(t, created, true)
	testutil.Equal(t, got.IsResident(), true)
	testutil.Equal(t, got.Name, model.ResidentTaskName)
	testutil.Equal(t, got.Project, "")
	testutil.Equal(t, got.Branch, "")
	testutil.Equal(t, got.Worktree, dir)
	testutil.Equal(t, got.Status, model.StatusInProgress)
	if got.ID == "" {
		t.Fatal("expected an id")
	}

	again, created2, err := d.EnsureResidentTask(dir)
	testutil.NoError(t, err)
	testutil.Equal(t, created2, false)
	testutil.Equal(t, again.ID, got.ID)

	r, err := d.ResidentTask()
	testutil.NoError(t, err)
	testutil.Equal(t, r.ID, got.ID)
}

func TestResidentTask_NoneReturnsNil(t *testing.T) {
	d := testDB(t)
	r, err := d.ResidentTask()
	testutil.NoError(t, err)
	if r != nil {
		t.Fatalf("expected nil, got %+v", r)
	}
}

func TestEnsureResidentTask_RejectsEmptyDir(t *testing.T) {
	d := testDB(t)
	_, _, err := d.EnsureResidentTask("")
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestResident_SecondRowImpossible(t *testing.T) {
	d := testDB(t)
	first, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)

	// Bypass every Go guard: the partial unique index alone must stop it.
	_, err = d.conn.Exec(`INSERT INTO tasks (id, name, kind, created_at) VALUES ('second', 'resident', 'resident', '2026-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatal("expected unique-index violation inserting a second resident")
	}
	r, _ := d.ResidentTask()
	testutil.Equal(t, r.ID, first.ID)
}

func TestResident_AddRejectsResidentKind(t *testing.T) {
	d := testDB(t)
	err := d.Add(&model.Task{Name: "x", Kind: model.KindResident, Status: model.StatusInProgress})
	if !errors.Is(err, ErrResidentProtected) {
		t.Fatalf("want ErrResidentProtected, got %v", err)
	}
	r, _ := d.ResidentTask()
	if r != nil {
		t.Fatal("resident row must not have been created")
	}
}

func TestResident_TasksListsExcludeIt(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "plain", Name: "plain", Status: model.StatusPending}))
	_, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)

	all, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(all), 1)
	testutil.Equal(t, all[0].ID, "plain")

	checked, err := d.TasksChecked()
	testutil.NoError(t, err)
	testutil.Equal(t, len(checked), 1)
}

func TestResident_GetStillResolvesByID(t *testing.T) {
	d := testDB(t)
	r, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)
	got, err := d.Get(r.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.IsResident(), true)
}

func TestResident_LifecycleWritesRejected(t *testing.T) {
	d := testDB(t)
	r, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)

	cases := []struct {
		name string
		do   func() error
	}{
		{"SetStatus complete", func() error { return d.SetStatus(r.ID, model.StatusComplete) }},
		{"SetStatus same", func() error { return d.SetStatus(r.ID, model.StatusInProgress) }},
		{"SetArchived", func() error { return d.SetArchived(r.ID, true) }},
		{"Delete", func() error { return d.Delete(r.ID) }},
		{"SetTaskAccount", func() error { return d.SetTaskAccount(r.ID, "work", false) }},
		{"SetPinned", func() error { return d.SetPinned(r.ID, true) }},
		{"Rename", func() error { return d.Rename(r.ID, "x") }},
		{"RenameIfName", func() error { _, err := d.RenameIfName(r.ID, r.Name, "x"); return err }},
		{"Update status", func() error {
			c := *r
			c.Status = model.StatusComplete
			return d.Update(&c)
		}},
		{"Update archived", func() error {
			c := *r
			c.Archived = true
			return d.Update(&c)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.do()
			if !errors.Is(err, ErrResidentProtected) {
				t.Fatalf("want ErrResidentProtected, got %v", err)
			}
		})
	}
	after, err := d.Get(r.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.Status, model.StatusInProgress)
	testutil.Equal(t, after.Archived, false)
}

func TestResident_UpdateRuntimeFieldsAllowedAndKindImmutable(t *testing.T) {
	d := testDB(t)
	r, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)

	c := *r
	c.SessionID = "sess-1"
	c.AgentPID = 42
	testutil.NoError(t, d.Update(&c))
	got, _ := d.Get(r.ID)
	testutil.Equal(t, got.SessionID, "sess-1")
	testutil.Equal(t, got.Kind, model.KindResident)

	// A caller zeroing Kind must not demote the row.
	c.Kind = ""
	testutil.NoError(t, d.Update(&c))
	got, _ = d.Get(r.ID)
	testutil.Equal(t, got.Kind, model.KindResident)

	// An ordinary task cannot be promoted.
	testutil.NoError(t, d.Add(&model.Task{ID: "p", Name: "p", Status: model.StatusPending}))
	p, _ := d.Get("p")
	p.Kind = model.KindResident
	if err := d.Update(p); !errors.Is(err, ErrResidentProtected) {
		t.Fatalf("want ErrResidentProtected promoting, got %v", err)
	}
}

func TestResident_PruneAndSweepQueriesSkipIt(t *testing.T) {
	d := testDB(t)
	r, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)

	pruned, _, err := d.PruneTasks([]string{r.ID})
	testutil.NoError(t, err)
	testutil.Equal(t, len(pruned), 0)
	if got, _ := d.Get(r.ID); got == nil {
		t.Fatal("resident row must survive PruneTasks")
	}

	// Even forced to complete+archived via raw SQL, it is never a candidate.
	_, err = d.conn.Exec(`UPDATE tasks SET status='in_review', archived=1 WHERE id=?`, r.ID)
	testutil.NoError(t, err)
	cands, err := d.StuckTaskCandidates()
	testutil.NoError(t, err)
	testutil.Equal(t, len(cands), 0)

	_, err = d.conn.Exec(`UPDATE tasks SET status='complete' WHERE id=?`, r.ID)
	testutil.NoError(t, err)
	pc, _, err := d.PruneCompleted()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pc), 0)

	found, err := d.FindByNameProject(model.ResidentTaskName, "")
	testutil.NoError(t, err)
	if found != nil {
		t.Fatal("FindByNameProject must not return the resident")
	}
}

func TestResident_NoTaskEventsOnCreate(t *testing.T) {
	var got []model.Event
	prev := events.SetSink(sinkFunc(func(ev model.Event) { got = append(got, ev) }))
	t.Cleanup(func() { events.SetSink(prev) })

	d := testDB(t)
	_, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 1)
	testutil.Equal(t, got[0].Type, model.EventTypeResidentCreated)
}

func TestResident_OpenMarksExistingResidentForEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	r, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)
	// events' resident-ID set is process-global, so EnsureResidentTask above has
	// already marked r.ID. Re-key the persisted row to an ID nothing has marked
	// so only db.Open's markExistingResident can make it resident-namespaced.
	freshID := "res-fresh-" + generateID()
	_, err = d.conn.Exec(`UPDATE tasks SET id=? WHERE id=?`, freshID, r.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Close())

	var got []model.Event
	prev := events.SetSink(sinkFunc(func(ev model.Event) { got = append(got, ev) }))
	t.Cleanup(func() { events.SetSink(prev) })

	// Control: before Open the fresh ID is a plain task ID.
	events.Emit(model.EventTypeSessionIdle, freshID, nil)
	testutil.Equal(t, got[len(got)-1].Type, string(model.EventTypeSessionIdle))

	d2, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d2.Close() })
	events.Emit(model.EventTypeSessionIdle, freshID, nil)
	testutil.Equal(t, got[len(got)-1].Type, "resident.session.idle")
}

// The Cleanup/prune sweeps must carry their own kind=” filter: none of them
// may surface the resident row even when its columns match the predicate.
func TestResident_SweepFiltersExcludeResidentRow(t *testing.T) {
	d := heraTestDB(t)
	r, _, err := d.EnsureResidentTask(residentDir(t))
	testutil.NoError(t, err)
	ctl := &model.Task{ID: "ctl", Name: "ctl", Status: model.StatusPending}
	testutil.NoError(t, d.Add(ctl))

	t.Run("StuckTaskCandidates", func(t *testing.T) {
		_, err := d.conn.Exec(`UPDATE tasks SET archived=1, status='in_review' WHERE id IN (?, ?)`, r.ID, ctl.ID)
		testutil.NoError(t, err)
		got, err := d.StuckTaskCandidates()
		testutil.NoError(t, err)
		testutil.Equal(t, len(got), 1)
		testutil.Equal(t, got[0].Task.ID, ctl.ID)
	})

	t.Run("HeraNukeReclaimCandidates", func(t *testing.T) {
		orch := mkOrch(t, d, "nuke")
		for _, id := range []string{r.ID, ctl.ID} {
			role := mkRole(t, d, orch.ID, "w-"+id, HeraKindWorker)
			b := bind(t, d, role.ID, id)
			// Seed a historical ended row directly: new lifecycle guards forbid
			// ending a resident binding through the public API.
			_, err := d.conn.Exec(`UPDATE hera_bindings SET ended_at=?, end_reason=? WHERE id=?`, formatTime(time.Now()), HeraEndReasonUserDeleted, b.ID)
			testutil.NoError(t, err)
		}
		got, err := d.HeraNukeReclaimCandidates()
		testutil.NoError(t, err)
		testutil.Equal(t, len(got), 1)
		testutil.Equal(t, got[0].ID, ctl.ID)
	})

	t.Run("PruneCompleted", func(t *testing.T) {
		_, err := d.conn.Exec(`UPDATE tasks SET status='complete' WHERE id IN (?, ?)`, r.ID, ctl.ID)
		testutil.NoError(t, err)
		pruned, _, err := d.PruneCompleted()
		testutil.NoError(t, err)
		testutil.Equal(t, len(pruned), 1)
		testutil.Equal(t, pruned[0].ID, ctl.ID)
		if got, _ := d.Get(r.ID); got == nil {
			t.Fatal("resident row must survive PruneCompleted")
		}
	})
}

type sinkFunc func(model.Event)

func (f sinkFunc) Emit(ev model.Event) { f(ev) }

func TestResident_SetTaskBackendMoveRejected(t *testing.T) {
	d := testDB(t)
	r, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = d.SetTaskBackendMove(r.ID, TaskBackendMove{Backend: "codex", Prompt: "seed"})
	testutil.ErrorIs(t, err, ErrResidentProtected)
	got, _ := d.Get(r.ID)
	testutil.Equal(t, got.Backend, r.Backend)
	testutil.Equal(t, got.Prompt, r.Prompt)
}
