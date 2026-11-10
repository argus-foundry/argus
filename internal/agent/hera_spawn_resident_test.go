package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
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
