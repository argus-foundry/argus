package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSpawnResidentWorkerMarkerAndCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := createTestDB(t, initGitRepo(t))
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
