## ADDED Requirements

### Requirement: Backend tier list endpoints

`GET /api/backend-tiers` SHALL return `{source, editable, revision, tiers[]}` where `source` is `database` or `config_file`, and each tier carries `backend`, `account`, `probe`, `threshold_pct` and `usage`. `usage` SHALL be null when routing has no valid reading, else `{percent_used, resets_at, measured_at, scope, account}` using the reading and scope routing scores that tier with (Codex: `scope:"global"`, `account:null`). `PUT /api/backend-tiers` SHALL accept `{"tiers":[...]}`, require `If-Match` equal to the current revision, validate every tier (known backend, account that exists and supports the backend's family, registered probe, threshold 0–100) before storing, and replace the whole ordered list in one transaction; an empty list clears it. It SHALL return 400 for invalid input, 409 when config.toml defines the list, 412 for a stale revision, 428 for a missing `If-Match`, and 200 with the effective list and new revision on success. `GET /api/backend-tiers/usage` SHALL return `{revision, readings:[{index, usage}]}`.

#### Scenario: Reorder and clear
- **WHEN** a valid list is saved with the current revision
- **THEN** the store holds the list in the given order and the response carries a new revision
- **AND** saving `[]` clears the stored tiers

#### Scenario: Invalid tier leaves the list intact
- **WHEN** any tier in the request is invalid
- **THEN** 400 is returned and the stored list and event stream are unchanged

#### Scenario: config.toml owns the list
- **WHEN** config.toml defines `[[backend_routing.tier]]`
- **THEN** GET reports `source:"config_file", editable:false` and PUT returns 409 without storing

#### Scenario: Unknown reading
- **WHEN** a tier has no valid reading (expired, none probe, named Codex account)
- **THEN** its `usage` is null, never zero

### Requirement: Backend tier change events

After a committed tier save the daemon SHALL emit `config.changed {"resource":"backend_tiers"}`. When the readings routing scores the configured tiers change, it SHALL emit `backend_tiers.usage_changed {"revision"}` with no usage values.
