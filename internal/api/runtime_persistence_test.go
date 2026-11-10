package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func runtimeFileServer(t *testing.T) (*Server, *db.DB, *sql.DB) {
	t.Helper()
	srv, _ := testServer(t)
	testutil.NoError(t, srv.Shutdown(context.Background()))
	path := filepath.Join(t.TempDir(), "runtime.sql")
	d, err := db.Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	srv.db = d
	observer, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = observer.Close() })
	return srv, d, observer
}

func TestRuntimePersistFailureRestoresCloseout(t *testing.T) {
	srv, d, observer := runtimeFileServer(t)
	task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
	testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
	testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
	before, err := d.HeraRoleStatusFor(role)
	testutil.NoError(t, err)
	_, err = observer.Exec(`CREATE TRIGGER fail_runtime BEFORE UPDATE OF prompt ON tasks WHEN NEW.prompt='updated' BEGIN SELECT RAISE(FAIL,'injected write failure'); END`)
	testutil.NoError(t, err)
	runner := &guardRunner{SessionRunner: srv.runner, startErr: errors.New("must not start")}
	srv.runner = runner
	_, rev := runtimeGet(t, srv, task.ID)
	sink := installAPISink(t)
	w := runtimePatch(srv, task.ID, `{"prompt":"updated","apply":"restart","force_revive":true}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusInternalServerError)
	testutil.Equal(t, decodeBody(t, w)["code"], any("store_failed"))
	after, err := d.HeraRoleStatusFor(role)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, before)
	testutil.Equal(t, readyMeta(t, d, task.ID), "true")
	testutil.Equal(t, runner.calls, 0)
	testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, runtimeOf(got), runtimeOf(task))
}

func TestRuntimeNoopPerformsZeroUpdates(t *testing.T) {
	srv, d, observer := runtimeFileServer(t)
	task := addStopped(t, d, nil)
	_, rev := runtimeGet(t, srv, task.ID)
	_, err := observer.Exec(`CREATE TABLE runtime_updates(n INTEGER); INSERT INTO runtime_updates VALUES(0); CREATE TRIGGER count_runtime AFTER UPDATE ON tasks BEGIN UPDATE runtime_updates SET n=n+1; END`)
	testutil.NoError(t, err)
	sink := installAPISink(t)
	w := runtimePatch(srv, task.ID, `{"prompt":"orig prompt","apply":"next_resume"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusOK)
	var updates int
	testutil.NoError(t, observer.QueryRow("SELECT n FROM runtime_updates").Scan(&updates))
	testutil.Equal(t, updates, 0)
	testutil.Equal(t, len(sink.events()), 0)
	testutil.Equal(t, decodeBody(t, w)["revision"], any(rev))
}

func TestRuntimeRecomputesSandboxed(t *testing.T) {
	for _, field := range []string{"override", "backend"} {
		t.Run(field, func(t *testing.T) {
			srv, d := testServer(t)
			testutil.NoError(t, d.SetBackend("plain", config.Backend{Command: "cat"}))
			task := addStopped(t, d, func(task *model.Task) { task.Sandboxed = true })
			_, rev := runtimeGet(t, srv, task.ID)
			body := `{"sandbox_override":"disabled","apply":"next_resume"}`
			if field == "backend" {
				body = `{"backend":"plain","apply":"next_resume"}`
			}
			w := runtimePatch(srv, task.ID, body, master(ifMatch(rev)))
			testutil.Equal(t, w.Code, http.StatusOK)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.False(t, got.Sandboxed)
		})
	}
}

func TestRuntimeKickDoesNotAttemptRollbackWrite(t *testing.T) {
	srv, d, observer := runtimeFileServer(t)
	task := addStopped(t, d, nil)
	_, rev := runtimeGet(t, srv, task.ID)
	_, err := observer.Exec(`CREATE TABLE rollback_attempts(n INTEGER); INSERT INTO rollback_attempts VALUES(0); CREATE TRIGGER fail_rollback BEFORE UPDATE OF prompt ON tasks WHEN NEW.prompt='orig prompt' BEGIN UPDATE rollback_attempts SET n=n+1; SELECT RAISE(FAIL,'rollback forbidden'); END`)
	testutil.NoError(t, err)
	runner := &runtimeKickRunner{SessionRunner: srv.runner, live: &runtimeKickSession{pid: 100}}
	runner.kick = func(*model.Task) error { return errors.New("lost kick reply") }
	srv.runner = runner
	sink := installAPISink(t)
	w := runtimePatch(srv, task.ID, `{"prompt":"updated","apply":"restart"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusInternalServerError)
	testutil.Equal(t, decodeBody(t, w)["code"], any("restart_unknown"))
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Prompt, "updated")
	events := runtimeChangedEvents(sink)
	testutil.Equal(t, len(events), 1)
	testutil.Equal(t, runner.starts, 0)
	var attempts int
	testutil.NoError(t, observer.QueryRow("SELECT n FROM rollback_attempts").Scan(&attempts))
	testutil.Equal(t, attempts, 0)
}
