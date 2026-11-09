package db

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResultAt_UpdatePreservesUnchangedResult(t *testing.T) {
	for _, change := range []string{"noop", "rename"} {
		t.Run(change, func(t *testing.T) {
			d := testDB(t)
			task := &model.Task{ID: "preserve", Name: "old", Result: `{"failed":true,"reason":"original"}`}
			testutil.NoError(t, d.Add(task))
			_, err := d.conn.Exec(`UPDATE tasks SET result_at='2001-01-01T00:00:00Z' WHERE id=?`, task.ID)
			testutil.NoError(t, err)
			if change == "rename" {
				task.Name = "new"
			}
			testutil.NoError(t, d.Update(task))
			want := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
			testutil.Equal(t, d.ResultAt(task.ID).Equal(want), true)
		})
	}
}

func TestResultAt_ChangedWritersAdvance(t *testing.T) {
	for _, writer := range []string{"SetResult", "Update"} {
		t.Run(writer, func(t *testing.T) {
			d := testDB(t)
			task := &model.Task{ID: "advance", Name: "n", Result: `{"failed":true,"reason":"old"}`}
			testutil.NoError(t, d.Add(task))
			_, err := d.conn.Exec(`UPDATE tasks SET result_at='2001-01-01T00:00:00Z' WHERE id=?`, task.ID)
			testutil.NoError(t, err)
			task.Result = `{"failed":true,"reason":"new"}`
			if writer == "Update" {
				testutil.NoError(t, d.Update(task))
			} else {
				testutil.NoError(t, d.SetResult(task.ID, task.Result))
			}
			testutil.Equal(t, d.ResultAt(task.ID).After(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)), true)
		})
	}
}

func TestResultAt_AtomicWriteFailure(t *testing.T) {
	for _, writer := range []string{"Add", "Update", "SetResult"} {
		t.Run(writer, func(t *testing.T) {
			d := testDB(t)
			task := &model.Task{ID: "atomic", Name: "n"}
			if writer != "Add" {
				testutil.NoError(t, d.Add(task))
			}
			sink := installRecordingSink(t)
			_, err := d.conn.Exec(`CREATE TRIGGER reject_timestamp BEFORE UPDATE OF result_at ON tasks BEGIN SELECT RAISE(ABORT, 'reject timestamp'); END`)
			testutil.NoError(t, err)
			_, err = d.conn.Exec(`CREATE TRIGGER reject_insert_timestamp BEFORE INSERT ON tasks WHEN NEW.result_at != '' BEGIN SELECT RAISE(ABORT, 'reject timestamp'); END`)
			testutil.NoError(t, err)
			task.Result = `{"failed":true,"reason":"new"}`
			write := func() error {
				switch writer {
				case "Add":
					return d.Add(task)
				case "Update":
					return d.Update(task)
				default:
					return d.SetResult(task.ID, task.Result)
				}
			}
			testutil.Error(t, write())
			stored, err := d.Get(task.ID)
			if writer == "Add" {
				testutil.ErrorIs(t, err, ErrTaskNotFound)
			} else {
				testutil.NoError(t, err)
				testutil.Equal(t, stored.Result, "")
			}
			testutil.Equal(t, d.ResultAt(task.ID).IsZero(), true)
			testutil.Equal(t, len(sink.types()), 0)
			_, err = d.conn.Exec(`DROP TRIGGER reject_timestamp`)
			testutil.NoError(t, err)
			_, err = d.conn.Exec(`DROP TRIGGER reject_insert_timestamp`)
			testutil.NoError(t, err)
			testutil.NoError(t, write())
			stored, err = d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.Result, task.Result)
			testutil.Equal(t, d.ResultAt(task.ID).IsZero(), false)
			testutil.Equal(t, len(sink.types()) > 0, true)
		})
	}
}

func TestResultAt_StampedOnChangeOnly(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "r1", Name: "n"}))
	testutil.Equal(t, d.ResultAt("r1").IsZero(), true)

	testutil.NoError(t, d.SetResult("r1", `{"failed":true}`))
	first := d.ResultAt("r1")
	testutil.Equal(t, first.IsZero(), false)

	testutil.NoError(t, d.SetResult("r1", `{"failed":true}`))
	testutil.Equal(t, d.ResultAt("r1").Equal(first), true)

	testutil.NoError(t, d.SetResult("r1", `{"failed":true,"reason":"x"}`))
	testutil.Equal(t, d.ResultAt("r1").After(first) || d.ResultAt("r1").Equal(first), true)

	times, err := d.ResultTimes()
	testutil.NoError(t, err)
	testutil.Equal(t, len(times), 1)
}

func TestResultAt_SurvivesArchive(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "r2", Name: "n"}))
	testutil.NoError(t, d.SetResult("r2", `{"failed":true}`))
	at := d.ResultAt("r2")
	testutil.NoError(t, d.SetArchived("r2", true))
	testutil.Equal(t, d.ResultAt("r2").Equal(at), true)
}
