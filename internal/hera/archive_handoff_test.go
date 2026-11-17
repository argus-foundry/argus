package hera

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func handoffOK(*model.Task) (HandoffInfo, bool, string) {
	return HandoffInfo{PRURL: "https://x/pr/1", TipSHA: "abc"}, true, ""
}
func handoffBad(*model.Task) (HandoffInfo, bool, string) {
	return HandoffInfo{}, false, "CI failing"
}

// archCoord adds a coordinator role so digests/board posts have a recipient.
func archCoord(t *testing.T, d *db.DB, orchID int64) *db.HeraRole {
	t.Helper()
	task := &model.Task{Name: "coord", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(task))
	r, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orchID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, task.ID, "/wt/coord")
	testutil.NoError(t, err)
	return r
}

func TestHandoffVerdict(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	archCoord(t, d, orch.ID)

	t.Run("in_review with handoff ok is archivable", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "a", model.StatusInReview)
		v, info := HandoffVerdict(d, handoffOK, r)
		testutil.Equal(t, v.Archivable, true)
		testutil.Equal(t, info.PRURL, "https://x/pr/1")
	})
	t.Run("in_progress refuses", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "b", model.StatusInProgress)
		v, _ := HandoffVerdict(d, handoffOK, r)
		testutil.Contains(t, v.Reason, "not in_review or complete")
	})
	t.Run("failing handoff check refuses with its reason", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "c", model.StatusInReview)
		v, _ := HandoffVerdict(d, handoffBad, r)
		testutil.Contains(t, v.Reason, "CI failing")
	})
	t.Run("nil check refuses (fail closed)", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "e", model.StatusInReview)
		v, _ := HandoffVerdict(d, nil, r)
		testutil.Equal(t, v.Archivable, false)
	})
	t.Run("blocked, outbox, dependents still refuse", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "f", model.StatusInReview)
		testutil.NoError(t, d.UpsertHeraRoleStatus(r.ID, db.HeraStatusBlocked))
		v, _ := HandoffVerdict(d, handoffOK, r)
		testutil.Contains(t, v.Reason, "blocked")
		_, r2 := archWorker(t, d, orch.ID, "g", model.StatusInReview)
		cs, _ := d.ListHeraRolesByKind(orch.ID, db.HeraKindCoordinator)
		_, err := d.SendHeraMessage(r2.ID, cs[0].ID, "report", "report", nil)
		testutil.NoError(t, err)
		v, _ = HandoffVerdict(d, handoffOK, r2)
		testutil.Contains(t, v.Reason, "not yet read")
	})
	t.Run("unread inbox no longer refuses", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "h", model.StatusInReview)
		cs, _ := d.ListHeraRolesByKind(orch.ID, db.HeraKindCoordinator)
		_, err := d.SendHeraMessage(cs[0].ID, r.ID, "hello", "hello", nil)
		testutil.NoError(t, err)
		v, _ := HandoffVerdict(d, handoffOK, r)
		testutil.Equal(t, v.Archivable, true)
		mv := ArchivableVerdict(d, gitOK, r) // merged still needs complete, but not on mail
		testutil.Contains(t, mv.Reason, "not complete")
	})
}

// The verdict and its transactional twin must agree: wherever the verdict
// accepts, the transaction commits, and wherever the tx would refuse a soft
// clause the verdict refuses first.
func TestVerdictAndTransactionAgree(t *testing.T) {
	type tc struct {
		name   string
		mutate func(t *testing.T, d *db.DB, r *db.HeraRole, task *model.Task, orchID, coordID int64)
	}
	cases := []tc{
		{"clean", func(*testing.T, *db.DB, *db.HeraRole, *model.Task, int64, int64) {}},
		{"blocked", func(t *testing.T, d *db.DB, r *db.HeraRole, _ *model.Task, _, _ int64) {
			testutil.NoError(t, d.UpsertHeraRoleStatus(r.ID, db.HeraStatusBlocked))
		}},
		{"pinned role", func(t *testing.T, d *db.DB, r *db.HeraRole, _ *model.Task, _, _ int64) {
			testutil.NoError(t, d.PinHeraRole(r.ID))
		}},
		{"outbox", func(t *testing.T, d *db.DB, r *db.HeraRole, _ *model.Task, _, coord int64) {
			_, err := d.SendHeraMessage(r.ID, coord, "x", "x", nil)
			testutil.NoError(t, err)
		}},
		{"dependent", func(t *testing.T, d *db.DB, r *db.HeraRole, _ *model.Task, orch, _ int64) {
			dep, err := d.CreateHeraPlannedRole(db.CreateHeraRoleInput{OrchestratorID: orch, Name: "dep", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "later"})
			testutil.NoError(t, err)
			testutil.NoError(t, d.AddHeraBlock(dep.ID, r.ID))
		}},
		{"other live role", func(t *testing.T, d *db.DB, r *db.HeraRole, task *model.Task, _, _ int64) {
			o2, _ := d.CreateHeraOrchestrator("o2", "")
			_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o2.ID, Name: "x", Kind: db.HeraKindFreelance, ArgusProject: "p"}, task.ID, task.Worktree)
			testutil.NoError(t, err)
		}},
	}
	for _, kind := range []db.ArchiveKind{db.ArchiveKindHandedOff, db.ArchiveKindForced} {
		for _, c := range cases {
			t.Run(string(kind)+"/"+c.name, func(t *testing.T) {
				d := archDB(t)
				orch, _ := d.CreateHeraOrchestrator("o", "master")
				coord := archCoord(t, d, orch.ID)
				task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
				c.mutate(t, d, r, task, orch.ID, coord.ID)
				r, _ = d.HeraRole(r.ID) // re-read: the mutation may have changed the role row
				var verdictOK bool
				if kind == db.ArchiveKindForced {
					verdictOK = ForcedVerdict(d, r).Archivable
				} else {
					v, _ := HandoffVerdict(d, handoffOK, r)
					verdictOK = v.Archivable
				}
				_, err := d.ArchiveHeraUnit(db.ArchiveUnitInput{RoleID: r.ID, Kind: kind, Reason: "r"})
				testutil.Equal(t, err == nil, verdictOK)
			})
		}
	}
}

func TestArchiveRoleWith(t *testing.T) {
	t.Run("falls back to handed_off when the merged predicate refuses", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		coord := archCoord(t, d, orch.ID)
		task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		_, err := d.SendHeraMessage(coord.ID, r.ID, "ping\nmore", "ping", nil)
		testutil.NoError(t, err)
		run := &fakeArchiveRunner{alive: true, idle: true}
		var digest *db.HeraMessage
		res := ArchiveRoleWith(d, run, gitBad, r, ArchiveOptions{Handoff: handoffOK, OnDigest: func(m *db.HeraMessage) { digest = m }})
		testutil.Equal(t, res.Archived, true)
		testutil.Equal(t, res.Kind, db.ArchiveKindHandedOff)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, true)
		testutil.Equal(t, digest != nil, true)
		testutil.DeepEqual(t, run.stopped, []string{task.ID})
		recs, _ := d.ArchiveRecordsForRole(r.ID)
		testutil.Equal(t, recs[0].Kind, db.ArchiveKindHandedOff)
		testutil.Equal(t, recs[0].PRURL, "https://x/pr/1")
	})
	t.Run("merged wins when both pass", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		res := ArchiveRoleWith(d, nil, gitOK, r, ArchiveOptions{Handoff: handoffOK})
		testutil.Equal(t, res.Kind, db.ArchiveKindMerged)
	})
	t.Run("both refusing reports both reasons", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		res := ArchiveRoleWith(d, nil, gitBad, r, ArchiveOptions{Handoff: handoffBad})
		testutil.Equal(t, res.Archived, false)
		testutil.Contains(t, res.Reason, "open PR")
		testutil.Contains(t, res.Reason, "CI failing")
	})
	t.Run("dry run reports the kind and writes nothing", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		res := ArchiveRoleWith(d, nil, gitBad, r, ArchiveOptions{Handoff: handoffOK, DryRun: true})
		testutil.Equal(t, res.Kind, db.ArchiveKindHandedOff)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
		recs, _ := d.ArchiveRecordsForRole(r.ID)
		testutil.Equal(t, len(recs), 0)
	})
	t.Run("handoff re-check before the tx refuses a late change", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		calls := 0
		flaky := func(*model.Task) (HandoffInfo, bool, string) {
			calls++
			if calls >= 2 {
				return HandoffInfo{}, false, "tip moved"
			}
			return HandoffInfo{}, true, ""
		}
		res := ArchiveRoleWith(d, nil, gitBad, r, ArchiveOptions{Handoff: flaky})
		testutil.Equal(t, res.Archived, false)
		testutil.Contains(t, res.Reason, "tip moved")
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
	})
	t.Run("forced needs a reason, skips checks and stops a busy session", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		task, r := archWorker(t, d, orch.ID, "w", model.StatusInProgress)
		run := &fakeArchiveRunner{alive: true, idle: false}
		res := ArchiveRoleWith(d, run, gitBad, r, ArchiveOptions{Force: true})
		testutil.Contains(t, res.Reason, "non-empty reason")
		res = ArchiveRoleWith(d, run, gitBad, r, ArchiveOptions{Force: true, Reason: "batch owns it"})
		testutil.Equal(t, res.Archived, true)
		testutil.Equal(t, res.Kind, db.ArchiveKindForced)
		testutil.DeepEqual(t, run.stopped, []string{task.ID})
		recs, _ := d.ArchiveRecordsForRole(r.ID)
		testutil.Equal(t, recs[0].Reason, "batch owns it")
	})
	t.Run("forced refuses pinned role", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		_, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		testutil.NoError(t, d.PinHeraRole(r.ID))
		r, _ = d.HeraRole(r.ID)
		res := ArchiveRoleWith(d, nil, nil, r, ArchiveOptions{Force: true, Reason: "x"})
		testutil.Equal(t, res.Archived, false)
		testutil.Contains(t, res.Reason, "pinned")
	})
}

func TestFormatArchiveResultsShowsKind(t *testing.T) {
	out := FormatArchiveResults([]ArchiveRoleResult{{Role: "a", Archived: true, Kind: db.ArchiveKindHandedOff}, {Role: "b", Archived: true, Kind: db.ArchiveKindForced}}, false)
	testutil.Contains(t, out, "a [handed off]")
	testutil.Contains(t, out, "b [forced]")
}

func TestUnarchiveRole(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	archCoord(t, d, orch.ID)
	task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
	res := UnarchiveRole(d, r)
	testutil.Equal(t, res.Archived, false)
	testutil.Contains(t, res.Reason, "not archived")
	testutil.Equal(t, ArchiveRoleWith(d, nil, gitBad, r, ArchiveOptions{Handoff: handoffOK}).Archived, true)
	r, _ = d.HeraRole(r.ID)
	res = UnarchiveRole(d, r)
	testutil.Equal(t, res.Archived, true)
	got, _ := d.Get(task.ID)
	testutil.Equal(t, got.Archived, false)
	testutil.Equal(t, got.Status, model.StatusInProgress)
}

func TestArchiveTaskGuard(t *testing.T) {
	t.Run("plain task is not bound", func(t *testing.T) {
		d := archDB(t)
		task := &model.Task{Name: "plain", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		out, _ := ArchiveTaskGuard(d, nil, gitBad, handoffBad, task.ID, false, "")
		testutil.Equal(t, out, GuardNotBound)
	})
	t.Run("passing verdict archives role and task together", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		out, res := ArchiveTaskGuard(d, nil, gitBad, handoffOK, task.ID, false, "")
		testutil.Equal(t, out, GuardArchived)
		testutil.Equal(t, res.Kind, db.ArchiveKindHandedOff)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, true)
		role, _ := d.HeraRole(r.ID)
		testutil.Equal(t, role.ArchivedAt != nil, true)
	})
	t.Run("refusal carries the hera reason and writes nothing", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		task, _ := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		out, res := ArchiveTaskGuard(d, nil, gitBad, handoffBad, task.ID, false, "")
		testutil.Equal(t, out, GuardRefused)
		testutil.Contains(t, res.Reason, "CI failing")
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
	})
	t.Run("force overrides", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		archCoord(t, d, orch.ID)
		task, _ := archWorker(t, d, orch.ID, "w", model.StatusInProgress)
		out, res := ArchiveTaskGuard(d, nil, gitBad, handoffBad, task.ID, true, "human override")
		testutil.Equal(t, out, GuardArchived)
		testutil.Equal(t, res.Kind, db.ArchiveKindForced)
	})
	t.Run("a coordinator-only task is not covered by the guard", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		c := archCoord(t, d, orch.ID)
		b, err := d.HeraLiveBindingByRole(c.ID)
		testutil.NoError(t, err)
		out, _ := ArchiveTaskGuard(d, nil, gitOK, handoffOK, b.ArgusTaskID, false, "")
		testutil.Equal(t, out, GuardNotBound)
	})
	t.Run("hidden role does not block a plain archive", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusInReview)
		testutil.NoError(t, d.ArchiveHeraRole(r.ID))
		out, _ := ArchiveTaskGuard(d, nil, gitBad, handoffBad, task.ID, false, "")
		testutil.Equal(t, out, GuardNotBound)
	})
}

// failingStore makes a lookup error fail closed.
type failingBindings struct{ ArchiveStore }

func (failingBindings) ListHeraLiveBindingsByTask(string) ([]*db.HeraBinding, error) {
	return nil, errors.New("boom")
}

func TestArchiveTaskGuardFailsClosedOnLookupError(t *testing.T) {
	out, res := ArchiveTaskGuard(failingBindings{}, nil, gitOK, handoffOK, "t", false, "")
	testutil.Equal(t, out, GuardRefused)
	testutil.Contains(t, res.Reason, "unreadable")
}
