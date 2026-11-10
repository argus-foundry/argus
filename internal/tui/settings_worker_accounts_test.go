package tui

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSettingsWorkerAccounts(t *testing.T) {
	sv := testSettingsViewWithConfigToml(t, "[accounts.work]\nclaude_config_dir = \"/tmp/work-claude\"\n[accounts.code]\ncodex_home = \"/tmp/work-codex\"\n[resident.worker_accounts]\nwork = false\n")
	sv.setCategory(catResident)
	selectRowInCategory(t, sv, catResident, srResident, resWorkerAccountPrefix+"work")
	testutil.Contains(t, sv.SelectedRow().label, "OFF (config.toml)")
	sv.handleEnter()
	testutil.Contains(t, sv.residentMsg, "read-only")
	for _, row := range sv.rows {
		testutil.False(t, row.key == resWorkerAccountPrefix+"code")
	}
	selectRowInCategory(t, sv, catResident, srResident, resWorkerAccountPrefix+"default")
	sv.handleEnter()
	testutil.False(t, sv.database.Config().Resident.WorkerAccounts["default"])
	testutil.False(t, sv.database.Config().Resident.WorkerAccounts["work"])
	raw, err := sv.accountDB().GetConfigValue("resident.worker_accounts")
	testutil.NoError(t, err)
	testutil.False(t, strings.Contains(raw, "work")) // file overrides never leak into DB
	sv.handleEnter()
	testutil.True(t, sv.database.Config().Resident.WorkerAccounts["default"])
	sv.remote = true
	sv.handleEnter()
	testutil.True(t, sv.database.Config().Resident.WorkerAccounts["default"])
}

func TestSettingsWorkerAccountsInvalidAndNull(t *testing.T) {
	sv := testSettingsView(t)
	testutil.True(t, sv.toggleResidentWorkerAccount("missing"))
	testutil.Contains(t, sv.residentMsg, "not eligible")
	testutil.NoError(t, sv.database.SetConfigValue("resident.worker_accounts", "null"))
	sv.toggleResidentWorkerAccount("default")
	testutil.False(t, sv.database.Config().Resident.WorkerAccounts["default"])
	testutil.NoError(t, sv.database.SetConfigValue("resident.worker_accounts", "{"))
	sv.toggleResidentWorkerAccount("default")
	testutil.Contains(t, sv.residentMsg, "Worker accounts:")
}
