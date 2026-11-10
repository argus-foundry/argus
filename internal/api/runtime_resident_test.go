package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentRuntime(t *testing.T) {
	srv, d := testServer(t)
	resident, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	runner := &guardRunner{SessionRunner: srv.runner, startErr: errors.New("must not start")}
	srv.runner = runner
	sink := installAPISink(t)
	get := doReq(srv.routes(), "GET", "/api/tasks/"+resident.ID+"/runtime", "", nil)
	testutil.Equal(t, get.Code, http.StatusNotFound)
	patch := runtimePatch(srv, resident.ID, `{"prompt":"unwanted edit","apply":"next_resume"}`, ifMatch(runtimeRevision(resident)))
	testutil.Equal(t, patch.Code, http.StatusConflict)
	testutil.Contains(t, patch.Body.String(), "resident")
	got, err := d.Get(resident.ID)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, runtimeOf(got), runtimeOf(resident))
	testutil.Equal(t, got.SessionID, resident.SessionID)
	testutil.Equal(t, len(sink.events()), 0)
	testutil.Equal(t, runner.calls, 0)
}

func TestOrdinaryRuntimeStillAllowed(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	_, rev := runtimeGet(t, srv, task.ID)
	w := runtimePatch(srv, task.ID, `{"prompt":"ordinary edit","apply":"next_resume"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusOK)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Prompt, "ordinary edit")
}
