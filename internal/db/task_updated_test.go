package db

import (
	"encoding/json"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func updatedFields(t *testing.T, s *recordingSink) [][]string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]string
	for _, ev := range s.events {
		if ev.Type != model.EventTypeTaskUpdated {
			continue
		}
		var p struct {
			Changed []string `json:"changed_fields"`
		}
		testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
		out = append(out, p.Changed)
	}
	return out
}

func TestTaskUpdatedEmission(t *testing.T) {
	newTask := func(t *testing.T, mut func(*model.Task)) (*DB, *model.Task, *recordingSink) {
		d := testDB(t)
		task := &model.Task{ID: "tu1", Name: "n", Status: model.StatusPending}
		if mut != nil {
			mut(task)
		}
		testutil.NoError(t, d.Add(task))
		return d, task, installRecordingSink(t)
	}

	t.Run("pin emits pinned", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		testutil.NoError(t, d.SetPinned(task.ID, true))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"pinned"}})
	})
	t.Run("pin restores archived task", func(t *testing.T) {
		d, task, sink := newTask(t, func(x *model.Task) { x.Archived = true })
		testutil.NoError(t, d.SetPinned(task.ID, true))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"archived", "pinned"}})
	})
	t.Run("repeat pin is silent", func(t *testing.T) {
		d, task, sink := newTask(t, func(x *model.Task) { x.Pinned = true })
		testutil.NoError(t, d.SetPinned(task.ID, true))
		testutil.Equal(t, len(updatedFields(t, sink)), 0)
	})
	t.Run("unpin of unpinned is silent", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		testutil.NoError(t, d.SetPinned(task.ID, false))
		testutil.Equal(t, len(updatedFields(t, sink)), 0)
	})
	t.Run("archive clears pin", func(t *testing.T) {
		d, task, sink := newTask(t, func(x *model.Task) { x.Pinned = true })
		testutil.NoError(t, d.SetArchived(task.ID, true))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"archived", "pinned"}})
	})
	t.Run("restore emits archived", func(t *testing.T) {
		d, task, sink := newTask(t, func(x *model.Task) { x.Archived = true })
		testutil.NoError(t, d.SetArchived(task.ID, false))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"archived"}})
	})
	t.Run("failed write emits nothing", func(t *testing.T) {
		d, _, sink := newTask(t, nil)
		testutil.ErrorIs(t, d.SetPinned("missing", true), ErrTaskNotFound)
		testutil.ErrorIs(t, d.SetArchived("missing", true), ErrTaskNotFound)
		testutil.ErrorIs(t, d.SetStatus("missing", model.StatusComplete), ErrTaskNotFound)
		testutil.ErrorIs(t, d.SetResult("missing", "x"), ErrTaskNotFound)
		testutil.ErrorIs(t, d.SetTaskAccount("missing", "a", false), ErrTaskNotFound)
		testutil.ErrorIs(t, d.Update(&model.Task{ID: "missing"}), ErrTaskNotFound)
		testutil.Equal(t, len(updatedFields(t, sink)), 0)
	})
	t.Run("rename", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		testutil.NoError(t, d.Rename(task.ID, "n"))
		testutil.Equal(t, len(updatedFields(t, sink)), 0)
		testutil.NoError(t, d.Rename(task.ID, "other"))
		ok, err := d.RenameIfName(task.ID, "other", "third")
		testutil.NoError(t, err)
		testutil.Equal(t, ok, true)
		ok, err = d.RenameIfName(task.ID, "stale", "x")
		testutil.NoError(t, err)
		testutil.Equal(t, ok, false)
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"name"}, {"name"}})
	})
	t.Run("status", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		testutil.NoError(t, d.SetStatus(task.ID, model.StatusInProgress))
		testutil.NoError(t, d.SetStatus(task.ID, model.StatusInProgress))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"status"}})
	})
	t.Run("result", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		testutil.NoError(t, d.SetResult(task.ID, `{"a":1}`))
		testutil.NoError(t, d.SetResult(task.ID, `{"a":1}`))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"result"}})
	})
	t.Run("account", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		testutil.NoError(t, d.SetTaskAccount(task.ID, "work", true))
		testutil.NoError(t, d.SetTaskAccount(task.ID, "work", true))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"account"}})
	})
	t.Run("full update emits only real diffs", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		cur, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.NoError(t, d.Update(cur))
		testutil.Equal(t, len(updatedFields(t, sink)), 0)
		cur.Name = "renamed"
		cur.Model = "opus"
		cur.SetPinned(true)
		testutil.NoError(t, d.Update(cur))
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"name", "pinned", "runtime"}})
	})
	t.Run("pr meta", func(t *testing.T) {
		d, task, sink := newTask(t, nil)
		wrote, err := d.SetMetaBatchIfChanged(task.ID, "pr", map[string]string{"state": "open"})
		testutil.NoError(t, err)
		testutil.Equal(t, wrote, true)
		_, err = d.SetMetaBatchIfChanged(task.ID, "pr", map[string]string{"state": "open"})
		testutil.NoError(t, err)
		_, err = d.SetMetaBatchIfChanged(task.ID, "hera", map[string]string{"k": "v"})
		testutil.NoError(t, err)
		testutil.DeepEqual(t, updatedFields(t, sink), [][]string{{"pr"}})
	})
}

func TestTaskUpdatedRuntime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.Task)
		count  int
	}{
		{"backend", func(task *model.Task) { task.Backend = "codex" }, 1},
		{"model", func(task *model.Task) { task.Model = "m" }, 1},
		{"session", func(task *model.Task) { task.SessionID = "s" }, 1},
		{"combined", func(task *model.Task) { task.Backend = "codex"; task.Model = "m"; task.SessionID = "s" }, 1},
		{"agent pid", func(task *model.Task) { task.AgentPID = 123 }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDB(t)
			testutil.NoError(t, d.Add(&model.Task{ID: "runtime", Name: "n"}))
			task, err := d.Get("runtime")
			testutil.NoError(t, err)
			sink := installRecordingSink(t)
			tc.mutate(task)
			testutil.NoError(t, d.Update(task))
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, got, task)
			fields := updatedFields(t, sink)
			testutil.Equal(t, len(fields), tc.count)
			if tc.count == 1 {
				testutil.DeepEqual(t, fields, [][]string{{"runtime"}})
			}
		})
	}
}
