package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func TestTaskMutationsPersistentSink(t *testing.T) {
	// A deadlocked setter also blocks DB cleanup, so bound it in a child process.
	if os.Getenv("TEST_TASK_MUTATIONS_SINK") != "1" {
		exe, err := os.Executable()
		testutil.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestTaskMutationsPersistentSink$", "-test.v")
		cmd.Env = append(os.Environ(), "TEST_TASK_MUTATIONS_SINK=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("persistent sink mutations failed: %v (deadline: %v)\n%s", err, ctx.Err(), out)
		}
		return
	}
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		name   string
		fields []string
		apply  func(*db.DB, *model.Task) error
	}{
		{"update", []string{"runtime"}, func(d *db.DB, task *model.Task) error {
			task.Backend, task.Model, task.SessionID = "codex", "m", "s"
			return d.Update(task)
		}},
		{"rename", []string{"name"}, func(d *db.DB, task *model.Task) error {
			task.Name = "renamed"
			return d.Rename(task.ID, task.Name)
		}},
		{"conditional rename", []string{"name"}, func(d *db.DB, task *model.Task) error {
			old := task.Name
			task.Name = "renamed"
			ok, err := d.RenameIfName(task.ID, old, task.Name)
			if err == nil && !ok {
				return errors.New("conditional rename did not write")
			}
			return err
		}},
		{"result", []string{"result"}, func(d *db.DB, task *model.Task) error {
			task.Result = "opaque result"
			err := d.SetResult(task.ID, task.Result)
			task.ResultAt = d.ResultAt(task.ID)
			return err
		}},
		{"archive", []string{"archived"}, func(d *db.DB, task *model.Task) error {
			task.SetArchived(true)
			return d.SetArchived(task.ID, true)
		}},
		{"pin", []string{"pinned"}, func(d *db.DB, task *model.Task) error {
			task.SetPinned(true)
			return d.SetPinned(task.ID, true)
		}},
		{"status", []string{"status"}, func(d *db.DB, task *model.Task) error {
			task.SetStatus(model.StatusInReview)
			return d.SetStatus(task.ID, task.Status)
		}},
		{"account", []string{"account"}, func(d *db.DB, task *model.Task) error {
			task.Account, task.AccountRouted = "work", true
			return d.SetTaskAccount(task.ID, task.Account, task.AccountRouted)
		}},
		{"pr metadata", []string{"pr"}, func(d *db.DB, task *model.Task) error {
			wrote, err := d.SetMetaBatchIfChanged(task.ID, "pr", map[string]string{"state": "open"})
			if err == nil && !wrote {
				return errors.New("PR metadata did not write")
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, d := testServer(t)
			testutil.NoError(t, d.Add(&model.Task{ID: "sink", Name: "n", Status: model.StatusPending}))
			want, err := d.Get("sink")
			testutil.NoError(t, err)
			prev := events.SetSink(srv)
			t.Cleanup(func() { events.SetSink(prev) })
			testutil.NoError(t, tc.apply(d, want))
			got, err := d.Get(want.ID)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, got, want)
			if tc.name == "pr metadata" {
				entries, err := d.ListMeta(want.ID, "pr")
				testutil.NoError(t, err)
				testutil.Equal(t, len(entries), 1)
				if len(entries) == 1 {
					testutil.Equal(t, entries[0].Key, "state")
					testutil.Equal(t, entries[0].Value, "open")
				}
			}
			rows, err := d.EventsSince(0, 0)
			testutil.NoError(t, err)
			var updates []model.Event
			for _, row := range rows {
				if row.Type == "task.updated" {
					updates = append(updates, *row)
				}
			}
			testutil.Equal(t, len(updates), 1)
			if len(updates) != 1 {
				return
			}
			testutil.Equal(t, updates[0].TaskID, want.ID)
			payload, err := json.Marshal(map[string][]string{"changed_fields": tc.fields})
			testutil.NoError(t, err)
			testutil.Equal(t, string(updates[0].Payload), string(payload))
		})
	}
}
