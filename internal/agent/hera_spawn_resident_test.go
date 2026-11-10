package agent

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSpawnResidentWorkerMarkerAndCap(t *testing.T) {
	sandboxAvailabilityForSignalTest(t, true)
	t.Setenv("HOME", t.TempDir())
	d := createTestDB(t, initGitRepo(t))
	testutil.NoError(t, d.SetSandboxEnabled(true))
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetConfigValue("resident.max_workers", "1"))
	in := HeraWorkerSpawnInput{OrchestratorID: o.ID, BaseName: "errand", Project: "proj", TaskPrompt: "do it"}
	spawned, err := SpawnHeraWorker(d, &fakeRunner{}, in)
	testutil.NoError(t, err)
	entries, err := d.ListMeta(spawned.Task.ID, "hera")
	testutil.NoError(t, err)
	marker := ""
	for _, e := range entries {
		if e.Key == db.HeraMetaKeyResidentWorker {
			marker = e.Value
		}
	}
	testutil.Equal(t, marker, "true")
	testutil.Equal(t, spawned.Task.Branch, "argus/resident/errand")
	_, err = SpawnHeraWorker(d, &fakeRunner{}, in)
	testutil.True(t, err != nil)
	testutil.Contains(t, err.Error(), "cap 1")
	testutil.Contains(t, err.Error(), "live workers: 1")
	testutil.NoError(t, d.EndHeraBinding(spawned.Binding.ID, "graduated"))
	_, err = SpawnHeraWorker(d, &fakeRunner{}, in)
	testutil.NoError(t, err)
}

func TestResidentWorkerOrientation(t *testing.T) {
	got := HeraWorkerOrientation("resident", "resident", true)
	for _, part := range []string{"hera_send(status=blocked)", "hera_inbox", "multiple PRs", "multiple workers", "Never self-promote", "graduate"} {
		t.Run(part, func(t *testing.T) { testutil.Contains(t, got, part) })
	}
}

func TestResidentSpawnAccountProvenance(t *testing.T) {
	sandboxAvailabilityForSignalTest(t, true)
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		name, explicit, disabled, want string
		routed                         bool
	}{
		{"automatic default", "", "work", "", true},
		{"automatic work", "", "default", "work", true},
		{"explicit work", "work", "default", "work", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := createTestDB(t, initGitRepo(t))
			testutil.NoError(t, d.SetSandboxEnabled(true))
			testutil.NoError(t, d.SetAccount("work", config.Account{ClaudeConfigDir: t.TempDir()}))
			testutil.NoError(t, d.SetConfigValue("resident.worker_accounts", `{"`+tc.disabled+`":false}`))
			res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
			testutil.NoError(t, err)
			o, err := d.EnsureResidentOrchestrator(res.ID)
			testutil.NoError(t, err)
			spawned, err := SpawnHeraWorker(d, &fakeRunner{}, HeraWorkerSpawnInput{OrchestratorID: o.ID, BaseName: "errand", Project: "proj", Account: tc.explicit})
			testutil.NoError(t, err)
			testutil.Equal(t, spawned.Task.Account, tc.want)
			testutil.Equal(t, spawned.Task.AccountRouted, tc.routed)
			stored, err := d.Get(spawned.Task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.AccountRouted, tc.routed)
		})
	}
}

// TestSpawnHeraWorkerAccountPinOrdinaryOrch pins the hera_spawn_worker account
// pin for NON-resident orchestrators: a valid pin lands on the stored task as an
// explicit (non-routed) account, and an invalid one fails before any task, role,
// binding or worktree exists.
func TestSpawnHeraWorkerAccountPinOrdinaryOrch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := createTestDB(t, initGitRepo(t))
	testutil.NoError(t, d.SetAccount("work", config.Account{ClaudeConfigDir: t.TempDir()}))
	testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude"}))
	orch, err := d.CreateHeraOrchestrator("ordinary", "")
	testutil.NoError(t, err)

	t.Run("configured account is pinned", func(t *testing.T) {
		spawned, err := SpawnHeraWorker(d, &fakeRunner{}, HeraWorkerSpawnInput{
			OrchestratorID: orch.ID, BaseName: "pinned", Project: "proj", TaskPrompt: "x", Account: "work", Backend: "claude",
		})
		testutil.NoError(t, err)
		stored, err := d.Get(spawned.Task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, stored.Account, "work")
		testutil.Equal(t, stored.AccountRouted, false)
	})

	// An explicit pin outranks the account inherited from a pinned
	// coordinator; without one the worker inherits the coordinator's pin.
	t.Run("explicit pin outranks coordinator account", func(t *testing.T) {
		testutil.NoError(t, d.SetAccount("personal", config.Account{ClaudeConfigDir: t.TempDir()}))
		pinned, err := d.CreateHeraOrchestrator("coord-pinned", "")
		testutil.NoError(t, err)
		coord := &model.Task{ID: "coord-personal", Name: "coord", Project: "proj", Account: "personal", AccountRouted: false, Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(coord))
		_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
			OrchestratorID: pinned.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "proj", Prompt: "x",
		}, coord.ID, "")
		testutil.NoError(t, err)

		for _, tc := range []struct{ name, account, want string }{
			{"inherits", "", "personal"},
			{"explicit", "work", "work"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				spawned, err := SpawnHeraWorker(d, &fakeRunner{}, HeraWorkerSpawnInput{
					OrchestratorID: pinned.ID, BaseName: "w-" + tc.name, Project: "proj", TaskPrompt: "x", Account: tc.account, Backend: "claude",
				})
				testutil.NoError(t, err)
				stored, err := d.Get(spawned.Task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, stored.Account, tc.want)
				testutil.Equal(t, stored.AccountRouted, false)
			})
		}
	})

	t.Run("unknown account fails with no side effects", func(t *testing.T) {
		tasksBefore, err := d.Tasks()
		testutil.NoError(t, err)
		rolesBefore, err := d.ListHeraRoles(orch.ID, true)
		testutil.NoError(t, err)
		_, err = SpawnHeraWorker(d, &fakeRunner{}, HeraWorkerSpawnInput{
			OrchestratorID: orch.ID, BaseName: "bogus", Project: "proj", TaskPrompt: "x", Account: "nope", Backend: "claude",
		})
		testutil.ErrorIs(t, err, ErrAccount)
		tasksAfter, err := d.Tasks()
		testutil.NoError(t, err)
		rolesAfter, err := d.ListHeraRoles(orch.ID, true)
		testutil.NoError(t, err)
		testutil.Equal(t, len(tasksAfter), len(tasksBefore))
		testutil.Equal(t, len(rolesAfter), len(rolesBefore))
		_, statErr := os.Stat(WorktreeDir("proj", "bogus"))
		testutil.True(t, os.IsNotExist(statErr))
	})
}
