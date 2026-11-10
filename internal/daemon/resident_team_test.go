package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// The recycle seed's team section reports, per WORKER role only: the
// scraper-flagged blocked state (NeedsInputIDs), the unread count of that
// worker's messages to the resident, and no line for the coordinator itself.
func TestResidentTeamSectionContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	res, _, err := database.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := database.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	coords, err := database.ListHeraRolesByKind(o.ID, db.HeraKindCoordinator)
	testutil.NoError(t, err)
	testutil.Equal(t, len(coords), 1)
	mk := func(id, name string) *db.HeraRole {
		testutil.NoError(t, database.Add(&model.Task{ID: id, Name: name, Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))
		role, _, err := database.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: name, Kind: db.HeraKindWorker, ArgusProject: "p"}, id, t.TempDir())
		testutil.NoError(t, err)
		return role
	}
	helper := mk("t-helper", "helper")
	mk("t-stuck", "stuckrole")
	_, err = hera.New(database, nil).Send(helper.ID, coords[0].ID, "body", "all good", nil)
	testutil.NoError(t, err)

	runner := &residentRecycleCaptureRunner{needs: []string{"t-stuck"}}
	cfg := recycleTestConfig()
	r := newTaskRecycleRunner(database, runner, func() config.Config { return cfg })
	got := r.residentTeamSection(res.ID)

	testutil.Contains(t, got, "- helper: project=p; status=")
	testutil.Contains(t, got, "blocked=false; unread=1; last status: all good")
	testutil.Contains(t, got, "- stuckrole: project=p; status=")
	testutil.Contains(t, got, "blocked=true; unread=0; last status: (none)")
	testutil.False(t, strings.Contains(got, "- "+coords[0].Name+":"))
}
