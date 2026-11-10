package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// membershipFixture returns a DB with the standing team, one ordinary
// orchestrator, and an ordinary task "outsider" with no bindings.
func membershipFixture(t *testing.T) (*DB, *HeraOrchestrator, *HeraOrchestrator) {
	t.Helper()
	d := testDB(t)
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	team, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	other, err := d.CreateHeraOrchestrator("other", "")
	testutil.NoError(t, err)
	testutil.NoError(t, d.Add(&model.Task{ID: "outsider", Name: "outsider", Status: model.StatusInProgress, Worktree: "/wt/outsider"}))
	return d, team, other
}

func countRows(t *testing.T, d *DB, table string) int {
	t.Helper()
	var n int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

func TestStandingTeamRefusesOrdinaryBindings(t *testing.T) {
	for _, tc := range []string{"join attach", "bind existing role", "explicit other orchestrator id", "legacy name collision"} {
		t.Run(tc, func(t *testing.T) {
			d, team, other := membershipFixture(t)
			target := team.ID
			if tc == "legacy name collision" {
				// An active orchestrator named resident with no resident binding is
				// still treated as the standing team until renamed.
				d2 := testDB(t)
				_, err := d2.conn.Exec(`INSERT INTO hera_orchestrators(name,created_at) VALUES ('resident','2026-11-09')`)
				testutil.NoError(t, err)
				legacy, err := d2.HeraOrchestratorByName("resident")
				testutil.NoError(t, err)
				testutil.NoError(t, d2.Add(&model.Task{ID: "outsider", Name: "outsider", Status: model.StatusInProgress}))
				d, target = d2, legacy.ID
			}
			roles, bindings := countRows(t, d, "hera_roles"), countRows(t, d, "hera_bindings")
			var err error
			switch tc {
			case "join attach", "legacy name collision":
				_, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: target, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
			case "bind existing role":
				r, rErr := d.CreateHeraRole(CreateHeraRoleInput{OrchestratorID: team.ID, Name: "planned", Kind: HeraKindWorker})
				testutil.NoError(t, rErr)
				roles++
				_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: r.ID, ArgusTaskID: "outsider", WorktreePath: "/wt/outsider"})
			case "explicit other orchestrator id":
				// A role of the standing team bound under a different orchestrator
				// id must not slip past the binding-orchestrator check.
				r, rErr := d.CreateHeraRole(CreateHeraRoleInput{OrchestratorID: team.ID, Name: "planned", Kind: HeraKindWorker})
				testutil.NoError(t, rErr)
				roles++
				_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: r.ID, OrchestratorID: other.ID, ArgusTaskID: "outsider", WorktreePath: "/wt/outsider"})
			}
			testutil.ErrorIs(t, err, ErrResidentProtected)
			testutil.Equal(t, countRows(t, d, "hera_roles"), roles)
			testutil.Equal(t, countRows(t, d, "hera_bindings"), bindings)
			yes, err := d.IsResidentWorker("outsider")
			testutil.NoError(t, err)
			testutil.False(t, yes)
		})
	}
}

func TestStandingTeamOrdinaryOrchestratorsUnaffected(t *testing.T) {
	d, _, other := membershipFixture(t)
	_, b, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: other.ID, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
	testutil.NoError(t, err)
	testutil.Equal(t, b.OrchestratorID, other.ID)
	yes, err := d.IsStandingTeamOrchestrator(other.ID)
	testutil.NoError(t, err)
	testutil.False(t, yes)
}

func TestStandingTeamMoveIntoAndOutOfRefused(t *testing.T) {
	t.Run("into", func(t *testing.T) {
		d, team, other := membershipFixture(t)
		_, b, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: other.ID, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.NoError(t, err)
		roles := countRows(t, d, "hera_roles")
		_, err = d.MoveHeraBinding(b.ID, CreateHeraRoleInput{OrchestratorID: team.ID, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.ErrorIs(t, err, ErrResidentProtected)
		live, err := d.HeraLiveBindingByTask("outsider")
		testutil.NoError(t, err)
		testutil.Equal(t, live.ID, b.ID)
		testutil.Equal(t, countRows(t, d, "hera_roles"), roles)
	})
	t.Run("out", func(t *testing.T) {
		d, team, other := membershipFixture(t)
		_, b, err := d.CreateResidentWorkerRoleWithBinding(CreateHeraRoleInput{OrchestratorID: team.ID, Name: "w", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.NoError(t, err)
		roles := countRows(t, d, "hera_roles")
		_, err = d.MoveHeraBinding(b.ID, CreateHeraRoleInput{OrchestratorID: other.ID, Name: "w", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.ErrorIs(t, err, ErrResidentProtected)
		live, err := d.HeraLiveBindingByTask("outsider")
		testutil.NoError(t, err)
		testutil.Equal(t, live.ID, b.ID)
		testutil.Equal(t, countRows(t, d, "hera_roles"), roles)
		yes, err := d.IsResidentWorker("outsider")
		testutil.NoError(t, err)
		testutil.True(t, yes)
	})
}

func TestCreateResidentWorkerRoleWithBinding(t *testing.T) {
	for _, tc := range []string{"worker", "coordinator kind", "ordinary orchestrator", "resident task"} {
		t.Run(tc, func(t *testing.T) {
			d, team, other := membershipFixture(t)
			in := CreateHeraRoleInput{OrchestratorID: team.ID, Name: "w", Kind: HeraKindWorker}
			taskID := "outsider"
			switch tc {
			case "coordinator kind":
				in.Kind = HeraKindCoordinator
			case "ordinary orchestrator":
				in.OrchestratorID = other.ID
			case "resident task":
				res, err := d.ResidentTask()
				testutil.NoError(t, err)
				taskID = res.ID
			}
			roles := countRows(t, d, "hera_roles")
			_, b, err := d.CreateResidentWorkerRoleWithBinding(in, taskID, "/wt/outsider")
			if tc != "worker" {
				testutil.ErrorIs(t, err, ErrResidentProtected)
				testutil.Equal(t, countRows(t, d, "hera_roles"), roles)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, b.OrchestratorID, team.ID)
			yes, err := d.IsResidentWorker("outsider")
			testutil.NoError(t, err)
			testutil.True(t, yes)
		})
	}
}

func TestEnsureResidentOrchestratorStillBindsResident(t *testing.T) {
	d := testDB(t)
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	b, err := d.HeraLiveBindingByTask(res.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, b.OrchestratorID, o.ID)
	yes, err := d.IsStandingTeamOrchestrator(o.ID)
	testutil.NoError(t, err)
	testutil.True(t, yes)
}

// TestStandingTeamMembershipSurvivesRename pins the live-resident-binding arm of
// standingTeamWhere: the team row is renamed so the name arm no longer matches,
// and every binding path into or out of it must still refuse.
func TestStandingTeamMembershipSurvivesRename(t *testing.T) {
	setup := func(t *testing.T) (*DB, *HeraOrchestrator, *HeraOrchestrator) {
		d, team, other := membershipFixture(t)
		_, err := d.conn.Exec(`UPDATE hera_orchestrators SET name='team-x' WHERE id=?`, team.ID)
		testutil.NoError(t, err)
		return d, team, other
	}
	t.Run("create role with binding", func(t *testing.T) {
		d, team, _ := setup(t)
		roles, bindings := countRows(t, d, "hera_roles"), countRows(t, d, "hera_bindings")
		_, _, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: team.ID, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.ErrorIs(t, err, ErrResidentProtected)
		testutil.Equal(t, countRows(t, d, "hera_roles"), roles)
		testutil.Equal(t, countRows(t, d, "hera_bindings"), bindings)
	})
	t.Run("bind existing role", func(t *testing.T) {
		d, team, _ := setup(t)
		r, err := d.CreateHeraRole(CreateHeraRoleInput{OrchestratorID: team.ID, Name: "planned", Kind: HeraKindWorker})
		testutil.NoError(t, err)
		bindings := countRows(t, d, "hera_bindings")
		_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: r.ID, ArgusTaskID: "outsider", WorktreePath: "/wt/outsider"})
		testutil.ErrorIs(t, err, ErrResidentProtected)
		testutil.Equal(t, countRows(t, d, "hera_bindings"), bindings)
	})
	t.Run("move into", func(t *testing.T) {
		d, team, other := setup(t)
		_, b, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: other.ID, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.NoError(t, err)
		_, err = d.MoveHeraBinding(b.ID, CreateHeraRoleInput{OrchestratorID: team.ID, Name: "x", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.ErrorIs(t, err, ErrResidentProtected)
		live, err := d.HeraLiveBindingByTask("outsider")
		testutil.NoError(t, err)
		testutil.Equal(t, live.ID, b.ID)
	})
	t.Run("move out", func(t *testing.T) {
		d, team, other := membershipFixture(t)
		_, b, err := d.CreateResidentWorkerRoleWithBinding(CreateHeraRoleInput{OrchestratorID: team.ID, Name: "w", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.NoError(t, err)
		_, err = d.conn.Exec(`UPDATE hera_orchestrators SET name='team-x' WHERE id=?`, team.ID)
		testutil.NoError(t, err)
		_, err = d.MoveHeraBinding(b.ID, CreateHeraRoleInput{OrchestratorID: other.ID, Name: "w", Kind: HeraKindWorker}, "outsider", "/wt/outsider")
		testutil.ErrorIs(t, err, ErrResidentProtected)
		live, err := d.HeraLiveBindingByTask("outsider")
		testutil.NoError(t, err)
		testutil.Equal(t, live.ID, b.ID)
	})
}
