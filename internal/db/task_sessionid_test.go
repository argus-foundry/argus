package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRecordInitialSessionWaitsForSQLiteWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sql")
	launcher, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = launcher.Close() })
	writer, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	task := &model.Task{Name: "before", Status: model.StatusPending}
	testutil.NoError(t, launcher.Add(task))

	ctx := context.Background()
	holder, err := writer.conn.Conn(ctx)
	testutil.NoError(t, err)
	t.Cleanup(func() {
		_, _ = holder.ExecContext(ctx, `ROLLBACK`)
		_ = holder.Close()
	})
	_, err = holder.ExecContext(ctx, `BEGIN IMMEDIATE`)
	testutil.NoError(t, err)
	_, err = holder.ExecContext(ctx, `UPDATE tasks SET name=? WHERE id=?`, "after", task.ID)
	testutil.NoError(t, err)

	type launchResult struct {
		row *model.Task
		err error
	}
	started := make(chan struct{})
	finished := make(chan launchResult, 1)
	go func() {
		close(started)
		row, err := launcher.RecordInitialSession(task.ID, 123, "", "session")
		finished <- launchResult{row, err}
	}()
	<-started
	// Hold the write lock long enough to catch a deferred read transaction
	// that upgrades and fails immediately instead of waiting for the writer.
	select {
	case result := <-finished:
		t.Fatalf("launch finished before the writer committed: %v", result.err)
	case <-time.After(200 * time.Millisecond):
	}
	_, err = holder.ExecContext(ctx, `COMMIT`)
	testutil.NoError(t, err)
	result := <-finished
	testutil.NoError(t, result.err)
	persisted, err := writer.Get(task.ID)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, result.row, persisted)
	testutil.Equal(t, result.row.Name, "after")
	testutil.Equal(t, result.row.Status, model.StatusInProgress)
	testutil.Equal(t, result.row.AgentPID, 123)
	testutil.Equal(t, result.row.SessionID, "session")
}

func TestSessionStopGenerationFencesResumedSession(t *testing.T) {
	d := testDB(t)
	task := &model.Task{Name: "original", Status: model.StatusInReview, SessionID: "prior"}
	testutil.NoError(t, d.Add(task))
	generation, err := d.SessionStopGeneration(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, generation, int64(0))

	testutil.NoError(t, d.RecordSessionStop(task.ID))
	currentGeneration, err := d.SessionStopGeneration(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, currentGeneration, generation+1)

	row, err := d.RecordResumedSessionIfUnstopped(task.ID, 13, "prior", "stopped", generation)
	testutil.NoError(t, err)
	testutil.Nil(t, row)
	row, err = d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInReview)
	testutil.Equal(t, row.AgentPID, 0)
	testutil.Equal(t, row.SessionID, "prior")

	testutil.NoError(t, d.Rename(task.ID, "edited"))
	row, err = d.RecordResumedSessionIfUnstopped(task.ID, 14, "prior", "resumed", currentGeneration)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)
	testutil.Equal(t, row.AgentPID, 14)
	testutil.Equal(t, row.SessionID, "resumed")
	testutil.Equal(t, row.Name, "edited")

	testutil.NoError(t, d.RecordSessionStop(task.ID))
	currentGeneration, err = d.SessionStopGeneration(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, currentGeneration, generation+2)
	row, err = d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInReview)
}

func TestRecordSessionStopPreservesResidentStatus(t *testing.T) {
	d := testDB(t)
	resident, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	generation, err := d.SessionStopGeneration(resident.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.RecordSessionStop(resident.ID))
	nextGeneration, err := d.SessionStopGeneration(resident.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, nextGeneration, generation+1)
	row, err := d.Get(resident.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)

	row, err = d.RecordResumedSessionIfUnstopped(resident.ID, 123, "", "", generation)
	testutil.NoError(t, err)
	testutil.Nil(t, row)
}

func TestRecordSessionStopRejectsMissingTask(t *testing.T) {
	d := testDB(t)
	testutil.ErrorIs(t, d.RecordSessionStop("missing"), ErrTaskNotFound)
	_, err := d.SessionStopGeneration("missing")
	testutil.ErrorIs(t, err, ErrTaskNotFound)
	_, err = d.RecordResumedSessionIfUnstopped("missing", 123, "", "", 0)
	testutil.ErrorIs(t, err, ErrTaskNotFound)
}

func TestFailedResumePreservesLaterStopAndSessionID(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stopped     bool
		generatedID string
		wantID      string
	}{
		{"later stop", true, "recorded", "recorded"},
		{"different generated ID", false, "discarded", "recorded"},
		{"matching generated ID", false, "recorded", ""},
		{"preexisting session ID", false, "", "recorded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDB(t)
			task := &model.Task{
				Name: "edited", SessionID: "recorded", Status: model.StatusInReview, StartedAt: time.Now(),
			}
			testutil.NoError(t, d.Add(task))
			generation, err := d.SessionStopGeneration(task.ID)
			testutil.NoError(t, err)
			if tc.stopped {
				testutil.NoError(t, d.RecordSessionStop(task.ID))
			}

			row, err := d.RecordFailedResumeIfUnstopped(task.ID, tc.generatedID, generation)
			testutil.NoError(t, err)
			if tc.stopped {
				testutil.Nil(t, row)
				row, err = d.Get(task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, row.Status, model.StatusInReview)
				testutil.False(t, row.StartedAt.IsZero())
			} else {
				testutil.Equal(t, row.Status, model.StatusPending)
				testutil.True(t, row.StartedAt.IsZero())
			}
			testutil.Equal(t, row.SessionID, tc.wantID)
			testutil.Equal(t, row.Name, "edited")
		})
	}
}

func TestSetSessionIDIfEmpty(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{Name: "t", Project: "p"}
	testutil.NoError(t, d.Add(task))

	wrote, err := d.SetSessionIDIfEmpty(task.ID, "first")
	testutil.NoError(t, err)
	testutil.True(t, wrote)
	wrote, err = d.SetSessionIDIfEmpty(task.ID, "second")
	testutil.NoError(t, err)
	testutil.True(t, !wrote)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.SessionID, "first")

	_, err = d.SetSessionIDIfEmpty("missing", "x")
	testutil.ErrorIs(t, err, ErrTaskNotFound)
	_, err = d.SetSessionIDIfEmpty("", "x")
	testutil.Error(t, err)
}

func TestReplaceSessionID(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{Name: "t", Project: "p", SessionID: "old"}
	testutil.NoError(t, d.Add(task))

	changed, err := d.ReplaceSessionID(task.ID, "new")
	testutil.NoError(t, err)
	testutil.True(t, changed)
	changed, err = d.ReplaceSessionID(task.ID, "new")
	testutil.NoError(t, err)
	testutil.True(t, !changed)
	got, _ := d.Get(task.ID)
	testutil.Equal(t, got.SessionID, "new")

	_, err = d.ReplaceSessionID("missing", "x")
	testutil.ErrorIs(t, err, ErrTaskNotFound)
	_, err = d.ReplaceSessionID(task.ID, "")
	testutil.Error(t, err)
}
