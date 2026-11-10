package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSetTaskRuntime(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{Name: "n", Prompt: "p", Backend: "claude", SessionID: "s1", Branch: "b", Pinned: true}
	testutil.NoError(t, d.Add(task))

	old, err := d.SetTaskRuntime(task.ID, TaskRuntimeUpdate{Backend: "codex", Model: "m", SandboxOverride: "enabled", Sandboxed: true, Prompt: "p2"})
	testutil.NoError(t, err)
	testutil.Equal(t, old.Backend, "claude")
	testutil.Equal(t, old.Prompt, "p")

	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Backend, "codex")
	testutil.Equal(t, got.Model, "m")
	testutil.Equal(t, got.SandboxOverride, "enabled")
	testutil.Equal(t, got.Sandboxed, true)
	testutil.Equal(t, got.Prompt, "p2")
	testutil.Equal(t, got.SessionID, "s1")
	testutil.Equal(t, got.Branch, "b")
	testutil.Equal(t, got.Pinned, true)

	t.Run("unknown task", func(t *testing.T) {
		_, err := d.SetTaskRuntime("ghost", TaskRuntimeUpdate{})
		testutil.ErrorIs(t, err, ErrTaskNotFound)
	})
}

func TestSetTaskRuntimeConditional(t *testing.T) {
	for _, field := range []string{"backend", "model", "sandbox", "prompt", "session"} {
		t.Run(field, func(t *testing.T) {
			d, err := OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			task := &model.Task{Name: "runtime", Backend: "claude", Prompt: "original"}
			testutil.NoError(t, d.Add(task))
			expected := *task
			switch field {
			case "backend":
				task.Backend = "codex"
			case "model":
				task.Model = "opus"
			case "sandbox":
				task.SandboxOverride = "disabled"
			case "prompt":
				task.Prompt = "concurrent"
			case "session":
				task.SessionID = "captured"
			}
			testutil.NoError(t, d.Update(task))
			_, err = d.SetTaskRuntime(task.ID, TaskRuntimeUpdate{Expected: &expected, Backend: "claude", Prompt: "overwrite"})
			testutil.ErrorIs(t, err, ErrTaskRuntimeChanged)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Backend, task.Backend)
			testutil.Equal(t, got.Model, task.Model)
			testutil.Equal(t, got.SandboxOverride, task.SandboxOverride)
			testutil.Equal(t, got.Prompt, task.Prompt)
			testutil.Equal(t, got.SessionID, task.SessionID)
		})
	}
	t.Run("successful recapture preserves unrelated writes", func(t *testing.T) {
		d, err := OpenInMemory()
		testutil.NoError(t, err)
		t.Cleanup(func() { _ = d.Close() })
		task := &model.Task{Name: "original", Backend: "claude"}
		testutil.NoError(t, d.Add(task))
		expected := *task
		task.Name = "renamed"
		testutil.NoError(t, d.Update(task))
		sid := "new-conversation"
		_, err = d.SetTaskRuntime(task.ID, TaskRuntimeUpdate{Expected: &expected, SessionID: &sid, Backend: "claude", Prompt: "updated"})
		testutil.NoError(t, err)
		got, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Name, "renamed")
		testutil.Equal(t, got.SessionID, sid)
		testutil.Equal(t, got.Prompt, "updated")
	})
}
