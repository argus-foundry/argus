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
		"resident.account":        "work", // legacy single account
	} {
		testutil.NoError(t, d.SetConfigValue(k, v))
	}
	got = d.Config().Resident
	testutil.Equal(t, got.Enabled, true)
	testutil.Equal(t, got.Name, "Dot")
	testutil.Equal(t, got.ContextBudget, 90000)
	testutil.Equal(t, got.MemoryDir, "mem")
	testutil.Equal(t, got.Model, "opus")
	testutil.DeepEqual(t, got.Accounts, []string{"work"}) // migrated to a one-element list

	// A non-positive/garbage budget keeps the default.
	testutil.NoError(t, d.SetConfigValue("resident.context_budget", "nope"))
	testutil.Equal(t, d.Config().Resident.ContextBudget, d.Config().Hera.CoordinatorContextBudget)
}

func TestConfigResidentAutoSwitchAccount(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = d.Close() }()

	testutil.True(t, d.Config().Resident.AutoSwitchAccount) // default on
	testutil.NoError(t, d.SetConfigValue("resident.auto_switch_account", "false"))
	testutil.False(t, d.Config().Resident.AutoSwitchAccount)
	testutil.NoError(t, d.SetConfigValue("resident.auto_switch_account", "true"))
	testutil.True(t, d.Config().Resident.AutoSwitchAccount)
}

func TestConfigResidentAccountsList(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = d.Close() }()

	testutil.Equal(t, len(d.Config().Resident.Accounts), 0) // unset: the default list applies downstream
	testutil.NoError(t, d.SetConfigValue("resident.account", "personal"))
	testutil.DeepEqual(t, d.Config().Resident.Accounts, []string{"personal"})
	// The list key wins over the legacy single value.
	testutil.NoError(t, d.SetConfigValue("resident.accounts", `["work","personal","auto"]`))
	testutil.DeepEqual(t, d.Config().Resident.Accounts, []string{"work", "personal", "auto"})
	// A garbage value is ignored (logged), not applied.
	testutil.NoError(t, d.SetConfigValue("resident.accounts", "not json"))
	testutil.Equal(t, len(d.Config().Resident.Accounts), 0)
}
