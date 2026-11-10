package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

// Deleting an account drops its resident worker toggle, so an account later
// re-created under the same name starts enabled like any new account.
func TestDeleteAccountDropsResidentWorkerToggle(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.SetAccount("alt", config.Account{ClaudeConfigDir: t.TempDir()}))
	testutil.NoError(t, d.SetAccount("work", config.Account{ClaudeConfigDir: t.TempDir()}))
	testutil.NoError(t, d.SetConfigValue(configKeyResidentWorkerAccounts, `{"alt":false,"work":false}`))

	testutil.NoError(t, d.DeleteAccount("alt"))
	cfg := d.Config()
	_, altSet := cfg.Resident.WorkerAccounts["alt"]
	testutil.False(t, altSet)
	enabled, workSet := cfg.Resident.WorkerAccounts["work"]
	testutil.True(t, workSet)
	testutil.False(t, enabled)
	_, ok := cfg.Accounts["alt"]
	testutil.False(t, ok)

	// Deleting an account with no toggle (or an unknown name) changes nothing.
	testutil.NoError(t, d.DeleteAccount("never"))
	_, workSet = d.Config().Resident.WorkerAccounts["work"]
	testutil.True(t, workSet)
}

// An unparseable stored value is left untouched (Config already ignores it)
// and does not block the account delete.
func TestDeleteAccountLeavesUnparseableWorkerToggles(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.SetAccount("alt", config.Account{ClaudeConfigDir: t.TempDir()}))
	testutil.NoError(t, d.SetConfigValue(configKeyResidentWorkerAccounts, `not json`))
	testutil.NoError(t, d.DeleteAccount("alt"))
	_, ok := d.Config().Accounts["alt"]
	testutil.False(t, ok)
	var raw string
	testutil.NoError(t, d.conn.QueryRow(`SELECT value FROM config WHERE key = ?`, configKeyResidentWorkerAccounts).Scan(&raw))
	testutil.Equal(t, raw, "not json")
}
