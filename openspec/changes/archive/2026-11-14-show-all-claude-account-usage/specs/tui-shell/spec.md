## ADDED Requirements

### Requirement: Status bar shows every Claude account's usage

When one or more non-default Claude-capable accounts are configured, the status bar usage segment SHALL render one entry per such account, labelled with the account name, between the default account's `cla` entry and the `cdx` entry (e.g. `cla 100%w · personal 40%s · cdx 53%`), in the configured account order. Each entry SHALL follow the same rules as `cla`: the fuller still-valid session or weekly percentage with its terse window suffix, and a literal `—` when the account's reading is absent or stale. The TUI SHALL read these from the daemon's per-account usage readings on the same `usageRecheckInterval` poll as the rest of the segment. With no non-default Claude accounts configured, the segment SHALL be unchanged. In `--remote` mode and the in-process-runner fallback, the behavior of the existing readout SHALL be unchanged.

#### Scenario: Named account with a fresh reading

- **WHEN** a `personal` Claude account is configured and the daemon reports its weekly usage as 40% and fresh
- **THEN** the status bar renders `personal 40%w` between the `cla` and `cdx` entries

#### Scenario: Named account with no reading

- **WHEN** a `personal` Claude account is configured and the daemon has no valid reading for it
- **THEN** the status bar renders `personal —`

#### Scenario: No named accounts

- **WHEN** only the default Claude account is configured
- **THEN** the status bar renders `cla … · cdx …` exactly as before
