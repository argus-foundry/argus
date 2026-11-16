package tui

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/apiclient"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/store"
)

func stubArchiveChecks(t *testing.T, handoffOK bool) {
	t.Helper()
	old := archiveChecks
	archiveChecks = func(func() config.Config) (hera.GitCheck, hera.HandoffCheck) {
		return func(*model.Task) (bool, string) { return false, "open PR" },
			func(*model.Task) (hera.HandoffInfo, bool, string) {
				if handoffOK {
					return hera.HandoffInfo{PRURL: "https://x/pr/1", TipSHA: "abc"}, true, ""
				}
				return hera.HandoffInfo{}, false, "CI failing"
			}
	}
	t.Cleanup(func() { archiveChecks = old })
}

// heraWorkerApp seeds a coordinator + an in_review hera worker and returns the App.
func heraWorkerApp(t *testing.T) (*App, *db.DB, *model.Task, *db.HeraRole) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	orch, err := d.CreateHeraOrchestrator("O", "master")
	testutil.NoError(t, err)
	ct := &model.Task{Name: "coord", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(ct))
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, ct.ID, "/wt/c")
	testutil.NoError(t, err)
	task := &model.Task{Name: "w", Status: model.StatusInProgress, Project: "p", Worktree: "/wt/w", Branch: "b"}
	testutil.NoError(t, d.Add(task))
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "p"}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetStatus(task.ID, model.StatusInReview))
	app.refreshTasks()
	return app, d, task, role
}

func fireArchive(t *testing.T, app *App, task *model.Task, archived bool) {
	t.Helper()
	toggled := *task
	toggled.Archived = archived // the task list flips the flag before firing OnArchive
	readUI(t, app.tapp, func() { app.tasklist.OnArchive(&toggled) })
}

func TestTUIArchive_HeraBoundHandedOffArchivesRoleAndTask(t *testing.T) {
	stubArchiveChecks(t, true)
	app, d, task, role := heraWorkerApp(t)
	_, stop := wireApp(t, app)
	defer stop()
	fireArchive(t, app, task, true)
	waitForCond(t, app.tapp, "task archived", func() bool {
		g, _ := d.Get(task.ID)
		return g != nil && g.Archived
	})
	r, _ := d.HeraRole(role.ID)
	testutil.Equal(t, r.ArchivedAt != nil, true)
	recs, _ := d.ArchiveRecordsForRole(role.ID)
	testutil.Equal(t, recs[0].Kind, db.ArchiveKindHandedOff)

	// Unarchive brings the role back with the task.
	fireArchive(t, app, task, false)
	g, _ := d.Get(task.ID)
	testutil.Equal(t, g.Archived, false)
	r, _ = d.HeraRole(role.ID)
	testutil.Nil(t, r.ArchivedAt)
}

func TestTUIArchive_RefusalShowsReasonAndForceConfirm(t *testing.T) {
	stubArchiveChecks(t, false)
	app, d, task, role := heraWorkerApp(t)
	_, stop := wireApp(t, app)
	defer stop()
	fireArchive(t, app, task, true)
	waitForCond(t, app.tapp, "confirm offered", func() bool { return app.mode == modeHeraConfirm })
	readUI(t, app.tapp, func() { testutil.Contains(t, app.header.Notice(), "CI failing") })
	g, _ := d.Get(task.ID)
	testutil.Equal(t, g.Archived, false)

	// Declining changes nothing.
	pressEsc(t, app)
	g, _ = d.Get(task.ID)
	testutil.Equal(t, g.Archived, false)

	// Confirming forces it, recorded as forced.
	fireArchive(t, app, task, true)
	waitForCond(t, app.tapp, "confirm offered again", func() bool { return app.mode == modeHeraConfirm })
	pressEnter(t, app)
	waitForCond(t, app.tapp, "forced archive", func() bool {
		g, _ := d.Get(task.ID)
		return g != nil && g.Archived
	})
	recs, _ := d.ArchiveRecordsForRole(role.ID)
	testutil.Equal(t, recs[0].Kind, db.ArchiveKindForced)
	testutil.Equal(t, recs[0].Reason, "forced from TUI")
}

func TestTUIArchive_PlainTaskArchivesAsBefore(t *testing.T) {
	stubArchiveChecks(t, false)
	app, d, _, _ := heraWorkerApp(t)
	plain := &model.Task{Name: "plain", Status: model.StatusComplete, Project: "p"}
	testutil.NoError(t, d.Add(plain))
	_, stop := wireApp(t, app)
	defer stop()
	fireArchive(t, app, plain, true)
	waitForCond(t, app.tapp, "plain archived", func() bool {
		g, _ := d.Get(plain.ID)
		return g != nil && g.Archived
	})
}

// forcerStore is a remote-mode store whose archive is refused with a hera 409
// and which supports the force override.
type forcerStore struct {
	store.Store
	err    error
	forced []string
}

func (f *forcerStore) SetArchived(string, bool) error { return f.err }
func (f *forcerStore) ForceArchive(id, reason string) error {
	f.forced = append(f.forced, id+":"+reason)
	return nil
}

func TestTUIArchive_RemoteHeraRefusalOffersForce(t *testing.T) {
	app, d, task, _ := heraWorkerApp(t)
	fs := &forcerStore{Store: d, err: &apiclient.Error{Status: http.StatusConflict, Method: "POST", Path: "/archive", Message: "CI failing. To override ..."}}
	app.db = fs
	_, stop := wireApp(t, app)
	defer stop()
	fireArchive(t, app, task, true)
	assertHeraConfirmMode(t, app)
	readUI(t, app.tapp, func() { testutil.Contains(t, app.header.Notice(), "CI failing") })
	pressEnter(t, app)
	testutil.DeepEqual(t, fs.forced, []string{task.ID + ":forced from TUI"})
}

func TestTUIArchive_RemoteOtherErrorIsQuiet(t *testing.T) {
	app, d, task, _ := heraWorkerApp(t)
	app.db = &forcerStore{Store: d, err: errors.New("boom")}
	_, stop := wireApp(t, app)
	defer stop()
	fireArchive(t, app, task, true)
	readUI(t, app.tapp, func() { testutil.True(t, app.heraConfirmModal == nil) })
}

func TestTUIArchive_LocalBackstopNotice(t *testing.T) {
	app, d, task, _ := heraWorkerApp(t)
	app.db = refusingStore{Store: d, err: db.ErrHeraRoleLive}
	_, stop := wireApp(t, app)
	defer stop()
	fireArchive(t, app, task, true)
	readUI(t, app.tapp, func() { testutil.Contains(t, app.header.Notice(), "hera role is still live") })
}

func TestTUIArchiveRunnerAdapter(t *testing.T) {
	r := agent.NewRunner(nil)
	a := tuiArchiveRunner{r}
	testutil.Equal(t, a.IsAlive("x"), false)
	testutil.Equal(t, a.IsIdle("x"), false)
	testutil.Equal(t, a.BlockedOnPrompt("x"), false)
	testutil.Equal(t, a.Stop("x") != nil, true) // no such session
}

func TestInboxShowsArchiveRecords(t *testing.T) {
	app, d, task, role := heraWorkerApp(t)
	_ = app
	_, err := d.ArchiveHeraUnit(db.ArchiveUnitInput{RoleID: role.ID, Kind: db.ArchiveKindForced, Reason: "batch owns it"})
	testutil.NoError(t, err)
	entries, err := loadInboxEntries(d, task.ID)
	testutil.NoError(t, err)
	var found bool
	for _, e := range entries {
		if e.Source == "archive" {
			found = true
			testutil.Equal(t, e.From, "w")
			testutil.Equal(t, e.Summary, "archived (forced): batch owns it")
			testutil.Equal(t, e.Unread, false)
		}
	}
	testutil.Equal(t, found, true)

	e := archiveRecordEntry(db.ArchiveRecord{Kind: db.ArchiveKindHandedOff, PRURL: "https://x/pr/1", SettleNote: "merged", SettledAt: time.Now()}, "w")
	testutil.Contains(t, e.Body, "PR: https://x/pr/1")
	testutil.Contains(t, e.Body, "merged")
	testutil.Equal(t, e.Summary, "archived (handed off)")
}
