package tui

import (
	"encoding/json"
	"slices"

	"github.com/argus-foundry/argus/internal/agent"
)

func (sv *SettingsView) toggleResidentWorkerAccount(account string) bool {
	if !sv.residentEditable() {
		return sv.residentNotify("Worker accounts are read-only in --remote mode")
	}
	d := sv.accountDB()
	cfg := d.Config() // refresh file provenance immediately before write
	if !slices.Contains(agent.ResidentWorkerAccounts(cfg), account) {
		return sv.residentNotify("Account is not eligible for resident workers")
	}
	if d.ResidentSources().WorkerAccounts[account] {
		return sv.residentNotify("Worker account is defined in config.toml — read-only")
	}
	// Persist ONLY DB values: copying the merged map would freeze file-only
	// toggles into Settings after their config.toml keys are removed.
	values := map[string]bool{}
	if raw, err := d.GetConfigValue("resident.worker_accounts"); err == nil && raw != "" {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return sv.residentNotify("Worker accounts: %v", err)
		}
	}
	if values == nil {
		values = map[string]bool{}
	}
	enabled, set := cfg.Resident.WorkerAccounts[account]
	values[account] = set && !enabled
	data, err := json.Marshal(values)
	if err != nil {
		return sv.residentNotify("Worker accounts: %v", err)
	}
	return sv.persistResident("resident.worker_accounts", string(data))
}
