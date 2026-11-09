package db

import (
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func TestHeraCoordinatorRequest_Lifecycle(t *testing.T) {
	d := testDB(t)

	out, _, err := d.BeginHeraCoordinatorRequest("r1", "h1")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestNew)

	out, _, err = d.BeginHeraCoordinatorRequest("r1", "h1")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestInFlight)

	out, _, err = d.BeginHeraCoordinatorRequest("r1", "other")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestConflict)

	testutil.NoError(t, d.CompleteHeraCoordinatorRequest("r1", `{"task_id":"t1"}`))

	out, resp, err := d.BeginHeraCoordinatorRequest("r1", "h1")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestReplay)
	testutil.Equal(t, resp, `{"task_id":"t1"}`)

	out, _, err = d.BeginHeraCoordinatorRequest("r1", "other")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestConflict)
}

func TestHeraCoordinatorRequest_ReleaseOnlyPending(t *testing.T) {
	d := testDB(t)

	_, _, err := d.BeginHeraCoordinatorRequest("r1", "h1")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReleaseHeraCoordinatorRequest("r1"))
	out, _, err := d.BeginHeraCoordinatorRequest("r1", "h2")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestNew)

	testutil.NoError(t, d.CompleteHeraCoordinatorRequest("r1", "{}"))
	testutil.NoError(t, d.ReleaseHeraCoordinatorRequest("r1"))
	out, _, err = d.BeginHeraCoordinatorRequest("r1", "h2")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestReplay)
}

func TestHeraCoordinatorRequest_CompleteUnknown(t *testing.T) {
	d := testDB(t)
	if err := d.CompleteHeraCoordinatorRequest("missing", "{}"); err == nil {
		t.Fatal("expected error completing unknown request")
	}
}

func TestHeraCoordinatorRequest_SurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	_, _, err = d.BeginHeraCoordinatorRequest("r1", "h1")
	testutil.NoError(t, err)
	testutil.NoError(t, d.CompleteHeraCoordinatorRequest("r1", `{"ok":1}`))
	testutil.NoError(t, d.Close())

	d2, err := Open(path)
	testutil.NoError(t, err)
	defer func() { _ = d2.Close() }()
	out, resp, err := d2.BeginHeraCoordinatorRequest("r1", "h1")
	testutil.NoError(t, err)
	testutil.Equal(t, out, HeraCoordRequestReplay)
	testutil.Equal(t, resp, `{"ok":1}`)
}
