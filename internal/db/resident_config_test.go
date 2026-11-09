package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestConfigReadsResidentKeys(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = d.Close() }()

	got := d.Config().Resident
	testutil.Equal(t, got.Enabled, false)
	testutil.Equal(t, got.Name, "")

	for k, v := range map[string]string{
		"resident.enabled":        "true",
		"resident.name":           "Dot",
		"resident.context_budget": "90000",
		"resident.memory_dir":     "mem",
		"resident.model":          "opus",
		"resident.account":        "work",
	} {
		testutil.NoError(t, d.SetConfigValue(k, v))
	}
	got = d.Config().Resident
	testutil.Equal(t, got.Enabled, true)
	testutil.Equal(t, got.Name, "Dot")
	testutil.Equal(t, got.ContextBudget, 90000)
	testutil.Equal(t, got.MemoryDir, "mem")
	testutil.Equal(t, got.Model, "opus")
	testutil.Equal(t, got.Account, "work")

	// A non-positive/garbage budget keeps the default.
	testutil.NoError(t, d.SetConfigValue("resident.context_budget", "nope"))
	testutil.Equal(t, d.Config().Resident.ContextBudget, d.Config().Hera.CoordinatorContextBudget)
}
