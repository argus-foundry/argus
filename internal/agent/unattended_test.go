package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUnattendedHideRestart(t *testing.T) {
	sandboxAvailabilityForSignalTest(t, true)
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "worker", Name: "worker", Project: "proj"}
	w.Backend, w.Worktree = "claude", t.TempDir()
	w.SessionID = "known-session"
	testutil.NoError(t, d.Add(w))
	_, binding, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "worker", Kind: db.HeraKindWorker}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetMeta(w.ID, "hera", db.HeraMetaKeyResidentWorker, "true"))
	testutil.NoError(t, d.SetArchived(w.ID, true))
	testutil.NoError(t, d.SetArchived(w.ID, false))
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "resume"}[resume], func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Sandbox.Enabled = false
			_, _, err := BuildCmd(w, cfg, resume, d.IsResidentWorker)
			testutil.True(t, err != nil)
			testutil.Contains(t, err.Error(), "requires the Argus sandbox")
			cfg.Sandbox.Enabled = true
			cmd, cleanup, err := BuildCmd(w, cfg, resume, d.IsResidentWorker)
			testutil.NoError(t, err)
			if cleanup != nil {
				defer cleanup()
			}
			for _, flag := range []string{"--dangerously-skip-permissions", "--disallowedTools", "AskUserQuestion", "EnterPlanMode", "ExitPlanMode"} {
				testutil.Contains(t, cmd.Args[len(cmd.Args)-1], flag)
			}
			if resume {
				testutil.Contains(t, cmd.Args[2], "--resume")
				testutil.Contains(t, cmd.Args[2], "known-session")
			}
		})
	}
	// Graduation ends membership and clears the marker; a supervisor-owned
	// relaunch must query again and restore normal flags even without a sandbox.
	_, err = d.GraduateHeraWorker(binding.ID)
	testutil.NoError(t, err)
	cfg := config.DefaultConfig()
	cfg.Sandbox.Enabled = false
	cfg.Defaults.PermissionMode = "plan"
	cmd, cleanup, err := BuildCmd(w, cfg, true, d.IsResidentWorker)
	testutil.NoError(t, err)
	if cleanup != nil {
		defer cleanup()
	}
	testutil.Contains(t, cmd.Args[2], "--permission-mode plan")
	testutil.False(t, strings.Contains(cmd.Args[2], "--disallowedTools"))
	testutil.False(t, strings.Contains(cmd.Args[2], "--dangerously-skip-permissions"))
}

func TestUnattendedEligibility(t *testing.T) {
	for _, name := range []string{"claude", "codex", "pi", "custom"} {
		t.Run(name, func(t *testing.T) { testutil.Equal(t, FamilyOf(name).UnattendedWorker != nil, name == "claude") })
	}
}

func TestUnattendedOrdinaryAndPinnedResume(t *testing.T) {
	sandboxAvailabilityForSignalTest(t, true)
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Sandbox.Enabled = true
	cfg.Defaults.PermissionMode = "plan"
	cfg.Backends["claude"] = config.Backend{Command: "claude --permission-mode plan"}
	cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: t.TempDir()}}
	cfg.Resident.WorkerAccounts = map[string]bool{"work": false}
	task := &model.Task{ID: "t", Backend: "claude", Account: "work", Worktree: t.TempDir()}
	for _, worker := range []bool{false, true} {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			cmd, cleanup, err := BuildCmd(task, cfg, true, func(string) (bool, error) { return worker, nil })
			testutil.NoError(t, err)
			if cleanup != nil {
				defer cleanup()
			}
			line := cmd.Args[2]
			testutil.Equal(t, strings.Contains(line, "--disallowedTools"), worker)
			testutil.Equal(t, strings.Contains(line, "--permission-mode plan"), !worker)
			testutil.Equal(t, strings.Contains(line, "--dangerously-skip-permissions"), worker)
			testutil.Contains(t, strings.Join(cmd.Env, "\n"), "CLAUDE_CONFIG_DIR="+cfg.Accounts["work"].ClaudeConfigDir)
		})
	}
	delete(cfg.Accounts, "work")
	_, _, err := BuildCmd(task, cfg, true, func(string) (bool, error) { return true, nil })
	testutil.True(t, err != nil)
	testutil.Contains(t, err.Error(), "work")
}

func TestUnattendedSpawnRefusesBeforeCreation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := createTestDB(t, initGitRepo(t))
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetSandboxEnabled(false))
	_, err = SpawnHeraWorker(d, &fakeRunner{}, HeraWorkerSpawnInput{OrchestratorID: o.ID, BaseName: "errand", Project: "proj", Backend: "claude"})
	testutil.Contains(t, err.Error(), "requires the Argus sandbox")
	count, err := d.ResidentWorkerCount()
	testutil.NoError(t, err)
	testutil.Equal(t, count, 0)
	tasks, err := d.Tasks()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 0) // resident is hidden
}

func TestUnattendedRunnerGuards(t *testing.T) {
	task := &model.Task{ID: "worker", Backend: "claude", Worktree: t.TempDir()}
	cfg := config.DefaultConfig()
	cfg.Sandbox.Enabled = false
	r := NewRunner(nil, func(string) (bool, error) { return true, nil })
	_, err := r.Start(task, cfg, 24, 80, true)
	testutil.Contains(t, err.Error(), "requires the Argus sandbox")
	testutil.Contains(t, r.KickRerender(task, cfg, 24, 80).Error(), "requires the Argus sandbox")
	testutil.Contains(t, r.Recycle(task, cfg, 24, 80).Error(), "requires the Argus sandbox")
	_, _, err = BuildCmd(task, cfg, false, func(string) (bool, error) { return false, fmt.Errorf("unreadable") })
	testutil.Contains(t, err.Error(), "identity")
}
