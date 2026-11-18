package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const hookTestSID = "01a1ebbc-ca71-7c31-b311-b3aa50da12d9"

func TestCodexSessionStartHookFlag(t *testing.T) {
	testutil.Equal(t, codexSessionStartHookFlag(""), "")
	got := codexSessionStartHookFlag("'/opt/argus' codex-session-hook")
	testutil.Contains(t, got, "--dangerously-bypass-hook-trust")
	testutil.Contains(t, got, shellQuote(`hooks.SessionStart=[{hooks=[{type="command",command="'/opt/argus' codex-session-hook",timeout=10}]}]`))
}

func TestBuildCmd_CodexSessionStartHook(t *testing.T) {
	old := sessionHookCommandFn
	sessionHookCommandFn = func() string { return "/bin/argus codex-session-hook" }
	t.Cleanup(func() { sessionHookCommandFn = old })
	cfg := testConfig()

	t.Run("fresh launch registers the hook before the prompt", func(t *testing.T) {
		task := &model.Task{Project: "myapp", Prompt: "fix the bug", Worktree: t.TempDir()}
		cmd, _, err := BuildCmd(task, cfg, false)
		testutil.NoError(t, err)
		testutil.Contains(t, cmd.Args[2], "--dangerously-bypass-hook-trust")
		testutil.Contains(t, cmd.Args[2], "codex-session-hook")
		testutil.True(t, strings.Index(cmd.Args[2], "codex-session-hook") < strings.Index(cmd.Args[2], "-- 'fix the bug'"))
	})
	t.Run("resume never registers it", func(t *testing.T) {
		task := &model.Task{Project: "myapp", SessionID: hookTestSID, Worktree: t.TempDir()}
		cmd, _, err := BuildCmd(task, cfg, true)
		testutil.NoError(t, err)
		testutil.True(t, !strings.Contains(cmd.Args[2], "hook"))
	})
	t.Run("non-codex backend is untouched", func(t *testing.T) {
		task := &model.Task{Prompt: "p", Worktree: t.TempDir()}
		cmd, _, err := BuildCmd(task, cfg, false)
		testutil.NoError(t, err)
		testutil.True(t, !strings.Contains(cmd.Args[2], "hook"))
	})
}

func TestRecordHookSessionID(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	wt := filepath.Join(t.TempDir(), "wt")

	t.Run("fills an empty codex id once", func(t *testing.T) {
		task := &model.Task{Name: "a", Project: "p", Worktree: wt, Backend: "codex"}
		testutil.NoError(t, d.Add(task))
		wrote, err := RecordHookSessionID(d, task.ID, hookTestSID, false)
		testutil.NoError(t, err)
		testutil.True(t, wrote)
		wrote, err = RecordHookSessionID(d, task.ID, "11111111-2222-4333-8444-555555555555", false)
		testutil.NoError(t, err)
		testutil.True(t, !wrote)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.SessionID, hookTestSID)
	})
	t.Run("a clear start replaces the id, a startup report does not", func(t *testing.T) {
		task := &model.Task{Name: "r", Project: "p", Worktree: wt, Backend: "codex"}
		testutil.NoError(t, d.Add(task))
		_, err := RecordHookSessionID(d, task.ID, hookTestSID, false)
		testutil.NoError(t, err)
		const newThread = "22222222-2222-4222-8222-222222222222"
		wrote, err := RecordHookSessionID(d, task.ID, newThread, false)
		testutil.NoError(t, err)
		testutil.True(t, !wrote)
		wrote, err = RecordHookSessionID(d, task.ID, newThread, true)
		testutil.NoError(t, err)
		testutil.True(t, wrote)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.SessionID, newThread)
		_, err = RecordHookSessionID(d, task.ID, "bad", true)
		testutil.Error(t, err)
	})
	t.Run("rejects a malformed id", func(t *testing.T) {
		task := &model.Task{Name: "b", Project: "p", Worktree: wt, Backend: "codex"}
		testutil.NoError(t, d.Add(task))
		_, err := RecordHookSessionID(d, task.ID, "not-a-uuid", false)
		testutil.Error(t, err)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.SessionID, "")
	})
	t.Run("rejects a non-codex task", func(t *testing.T) {
		task := &model.Task{Name: "c", Project: "p", Worktree: wt, Backend: "claude"}
		testutil.NoError(t, d.Add(task))
		_, err := RecordHookSessionID(d, task.ID, hookTestSID, false)
		testutil.Error(t, err)
	})
	t.Run("unknown task", func(t *testing.T) {
		_, err := RecordHookSessionID(d, "nope", hookTestSID, false)
		testutil.Error(t, err)
	})
}
