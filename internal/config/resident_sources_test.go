package config

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestWorkerAccountOverlay(t *testing.T) {
	path := writeFile(t, "[resident.worker_accounts]\nwork = false\n")
	loader := NewFileLoader(path)
	base := DefaultConfig()
	base.Resident.WorkerAccounts = map[string]bool{"work": true, "personal": false}
	cfg := loader.Apply(base)
	testutil.False(t, cfg.Resident.WorkerAccounts["work"])
	testutil.False(t, cfg.Resident.WorkerAccounts["personal"])
	testutil.True(t, base.Resident.WorkerAccounts["work"])
	sources := loader.ResidentSources()
	testutil.True(t, sources.WorkerAccounts["work"])
	testutil.False(t, sources.WorkerAccounts["personal"])
	delete(sources.WorkerAccounts, "work")
	testutil.True(t, loader.ResidentSources().WorkerAccounts["work"])
	testutil.NoError(t, os.Remove(path))
	cfg = loader.Apply(base)
	testutil.True(t, cfg.Resident.WorkerAccounts["work"])
	testutil.Equal(t, len(loader.ResidentSources().WorkerAccounts), 0)
}
