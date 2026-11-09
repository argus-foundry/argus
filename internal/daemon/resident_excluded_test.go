package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/heraadopt"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// residentEnv is the seeded world every choke point runs against: one ordinary
// task and the resident row.
type residentEnv struct {
	db       *db.DB
	resident *model.Task
	plain    *model.Task
	home     string
}

func newResidentEnv(t *testing.T) *residentEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	plain := &model.Task{ID: "plain", Name: "plain", Status: model.StatusInProgress, Branch: "argus/plain"}
	testutil.NoError(t, d.Add(plain))
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	_, err = d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	return &residentEnv{db: d, resident: res, plain: plain, home: home}
}

// finishedResident is the resident row as a status-driven mechanism would see
// it if the DB filter were bypassed: finished, archived, branched, with a
// worktree under a managed root.
func (e *residentEnv) finishedResident(worktree string) *model.Task {
	r := *e.resident
	r.Status = model.StatusComplete
	r.Archived = true
	r.Branch = "argus/resident"
	r.Worktree = worktree
	return &r
}

func containsID(ts []*model.Task, id string) bool {
	for _, t := range ts {
		if t.ID == id {
			return true
		}
	}
	return false
}

// TestResidentExcludedEverywhere is the enumerating guard for the resident row
// (add-resident-agent, design D2): every task enumeration, sweeper, reaper and
// lifecycle choke point is listed here and run against a seeded resident row.
// A future enumeration that forgets the predicate belongs in this table.
//
// REST and MCP surfaces are listed by name and tested in their own packages
// (they need unexported server internals): internal/api
// TestResident_REST*, TestResident_NeedsInputSetExcludesResident;
// internal/mcp TestResident_*.
func TestResidentExcludedEverywhere(t *testing.T) {
	cases := []struct {
		name string
		// check returns an empty string when the resident was excluded, else a
		// description of the leak.
		check func(t *testing.T, e *residentEnv) string
	}{
		{"Hera model and jump roster", func(t *testing.T, e *residentEnv) string {
			m, err := heramodel.BuildModel(e.db, nil, nil, nil, nil)
			testutil.NoError(t, err)
			if len(m.Active)+len(m.Pinned)+len(m.Archived)+len(m.Freelance) > 0 {
				return "standing orchestrator listed"
			}
			return ""
		}},
		{"startup binding reconciliation", func(t *testing.T, e *residentEnv) string {
			before, err := e.db.HeraLiveBindingByTask(e.resident.ID)
			testutil.NoError(t, err)
			n, err := heraadopt.ReconcileBindings(e.db)
			testutil.NoError(t, err)
			after, err := e.db.HeraLiveBindingByTask(e.resident.ID)
			testutil.NoError(t, err)
			if n != 0 || after.ID != before.ID {
				return "resident binding ended"
			}
			return ""
		}},
		{"db.Tasks", func(t *testing.T, e *residentEnv) string {
			ts, err := e.db.Tasks()
			testutil.NoError(t, err)
			if containsID(ts, e.resident.ID) {
				return "listed"
			}
			return ""
		}},
		{"db.TasksChecked", func(t *testing.T, e *residentEnv) string {
			ts, err := e.db.TasksChecked()
			testutil.NoError(t, err)
			if containsID(ts, e.resident.ID) {
				return "listed"
			}
			return ""
		}},
		{"db.StuckTaskCandidates", func(t *testing.T, e *residentEnv) string {
			cs, err := e.db.StuckTaskCandidates()
			testutil.NoError(t, err)
			for _, c := range cs {
				if c.Task.ID == e.resident.ID {
					return "candidate"
				}
			}
			return ""
		}},
		{"db.HeraNukeReclaimCandidates", func(t *testing.T, e *residentEnv) string {
			ts, err := e.db.HeraNukeReclaimCandidates()
			testutil.NoError(t, err)
			if containsID(ts, e.resident.ID) {
				return "candidate"
			}
			return ""
		}},
		{"db.FindByNameProject", func(t *testing.T, e *residentEnv) string {
			got, err := e.db.FindByNameProject(model.ResidentTaskName, "")
			testutil.NoError(t, err)
			if got != nil {
				return "found"
			}
			return ""
		}},
		{"db.PruneCompleted and PruneTasks", func(t *testing.T, e *residentEnv) string {
			p1, _, err := e.db.PruneCompleted()
			testutil.NoError(t, err)
			p2, _, err := e.db.PruneTasks([]string{e.resident.ID})
			testutil.NoError(t, err)
			if containsID(p1, e.resident.ID) || containsID(p2, e.resident.ID) {
				return "pruned"
			}
			if r, _ := e.db.ResidentTask(); r == nil {
				return "row deleted"
			}
			return ""
		}},
		{"agent.ReconcileStaleSessionsExcept", func(t *testing.T, e *residentEnv) string {
			flipped, err := agent.ReconcileStaleSessionsExcept(e.db, nil)
			testutil.NoError(t, err)
			for _, id := range flipped {
				if id == e.resident.ID {
					return "flipped to in_review"
				}
			}
			if r, _ := e.db.ResidentTask(); r.Status != model.StatusInProgress {
				return "status changed"
			}
			return ""
		}},
		{"orphan worktree sweep", func(t *testing.T, e *residentEnv) string {
			// The resident dir is outside the managed worktree root, so the
			// sweep never walks it; it also stays in the known-path set so a
			// sweep could never classify it as an orphan.
			known, err := e.db.WorktreePaths()
			testutil.NoError(t, err)
			if !known[e.resident.Worktree] {
				return "resident dir not protected as a known path"
			}
			// Structural guard: the resident dir must lie outside the managed
			// worktree root, which is what keeps the real sweep off it.
			root := filepath.Dir(filepath.Dir(agent.WorktreeDir("p", "t"))) + string(filepath.Separator)
			if strings.HasPrefix(filepath.Clean(e.resident.Worktree)+string(filepath.Separator), root) {
				return fmt.Sprintf("resident dir %s is inside the managed worktree root %s", e.resident.Worktree, root)
			}
			// Walk-runs sanity (not the exclusion proof): an unknown sibling under the same parent as the
			// resident dir is counted, proving the walk really runs there, while
			// the resident dir itself (protected via known paths) adds nothing.
			parent := filepath.Dir(e.resident.Worktree)
			testutil.NoError(t, os.MkdirAll(e.resident.Worktree, 0o755))
			testutil.NoError(t, os.MkdirAll(filepath.Join(parent, "stray-proj", "stray-task"), 0o755))
			if n := agent.CountOrphanedWorktrees(parent, known); n != 1 {
				return fmt.Sprintf("sweep counted %d orphans, want exactly the 1 stray sibling", n)
			}
			if _, err := os.Stat(e.resident.Worktree); err != nil {
				return "resident dir missing after sweep"
			}
			return ""
		}},
		{"finished-session reaper", func(t *testing.T, e *residentEnv) string {
			r := &fakeReapRunner{running: []string{e.resident.ID}, idle: []string{e.resident.ID}}
			tasks := fakeReapTasks{e.resident.ID: e.finishedResident(e.resident.Worktree)}
			f := newReaper(r, tasks)
			for i := 0; i < 4; i++ {
				f.tick()
			}
			if len(r.stopped) > 0 {
				return "session stopped"
			}
			return ""
		}},
		{"devstack sweeper", func(t *testing.T, e *residentEnv) string {
			wt := dsRoot + "/Sketch/resident"
			h := newDSHarness(e.finishedResident(wt))
			h.proc(wt)
			ticks(h, 8)
			if len(h.stops) > 0 {
				return "dev stack stopped"
			}
			return ""
		}},
		{"PR poller eligibility", func(t *testing.T, e *residentEnv) string {
			if prPollEligible(e.finishedResident("/x")) {
				return "eligible"
			}
			if !prPollEligible(e.plain) {
				return "control failed: plain task must stay eligible"
			}
			return ""
		}},
		{"host-suspend advisory", func(t *testing.T, e *residentEnv) string {
			sent := sendHostSuspendSignals(e.db, []string{e.resident.ID, e.plain.ID}, 0)
			if sent != 1 {
				return "advisory sent to resident (or control failed)"
			}
			return ""
		}},
		{"bounce signals", func(t *testing.T, e *residentEnv) string {
			sent := sendBounceSignals(e.db, []string{e.resident.ID, e.plain.ID})
			if sent != 1 {
				return fmt.Sprintf("bounce signal sent to %d tasks, want only the plain control", sent)
			}
			msgs, err := e.db.Inbox(e.resident.ID, db.InboxFilter{})
			if err == nil && len(msgs) > 0 {
				return "resident inbox received ARGUS_BOUNCED"
			}
			return ""
		}},
		{"exit transition", func(t *testing.T, e *residentEnv) string {
			d := &Daemon{db: e.db}
			d.transitionTaskOnExit(e.resident.ID, true)
			d.transitionTaskOnExit(e.resident.ID, false)
			if r, _ := e.db.ResidentTask(); r.Status != model.StatusInProgress {
				return "status flipped on exit: " + r.Status.String()
			}
			return ""
		}},
		// Lifecycle verbs reject with ErrResidentProtected and change nothing.
		{"verb: complete (SetStatus)", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.SetStatus(e.resident.ID, model.StatusComplete))
		}},
		{"verb: set-status (Update)", func(t *testing.T, e *residentEnv) string {
			c := *e.resident
			c.Status = model.StatusInReview
			return wantProtected(e.db.Update(&c))
		}},
		{"verb: archive", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.SetArchived(e.resident.ID, true))
		}},
		{"verb: pin", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.SetPinned(e.resident.ID, true))
		}},
		{"verb: delete", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.Delete(e.resident.ID))
		}},
		{"verb: account move", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.SetTaskAccount(e.resident.ID, "work", false))
		}},
		{"verb: rename", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.Rename(e.resident.ID, "x"))
		}},
		{"verb: create a second (Add)", func(t *testing.T, e *residentEnv) string {
			return wantProtected(e.db.Add(&model.Task{Name: "second", Kind: model.KindResident}))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newResidentEnv(t)
			if leak := tc.check(t, e); leak != "" {
				t.Fatalf("resident leaked through %s: %s", tc.name, leak)
			}
			after, err := e.db.ResidentTask()
			testutil.NoError(t, err)
			_, err = e.db.HeraLiveBindingByTask(e.resident.ID)
			testutil.NoError(t, err)
			if after == nil || after.Status != model.StatusInProgress || after.Archived || after.Name != model.ResidentTaskName {
				t.Fatalf("resident row changed: %+v", after)
			}
		})
	}
}

func wantProtected(err error) string {
	if errors.Is(err, db.ErrResidentProtected) {
		return ""
	}
	return "not rejected with ErrResidentProtected: " + errString(err)
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
