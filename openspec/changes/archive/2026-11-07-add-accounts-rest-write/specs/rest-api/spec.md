## ADDED Requirements

### Requirement: Account management endpoints

The server SHALL let the master token create, edit and delete database-owned accounts, and SHALL report each account's field source and editability.

#### Scenario: Create

- **WHEN** the master token POSTs a valid new account
- **THEN** the response is 201 with the effective account, the row is stored and `config.changed` `{"resource":"accounts","name":"<name>"}` is emitted

#### Scenario: Non-master write

- **WHEN** a device token POSTs, PATCHes or DELETEs an account
- **THEN** the response is 403 and nothing is stored

#### Scenario: Invalid or reserved input

- **WHEN** the name or a directory is invalid, the name is `default`, or a directory is shared with another account
- **THEN** the response is 400 and nothing is stored

#### Scenario: config.toml ownership

- **WHEN** an account is defined in config.toml
- **THEN** its `source` fields are `config_file`, `editable` is false, and PATCH or DELETE return 409

#### Scenario: Delete while referenced

- **WHEN** a task (including resident and archived rows), default, project or artifact-home mapping, configured resident account or routing tier names the account
- **THEN** DELETE returns 409 with a `references` body and the account is kept. An incomplete reference read returns 500 and leaves the account unchanged

#### Scenario: Concurrent field edits

- **WHEN** Settings and REST edit different fields of the same account
- **THEN** each edit is merged with the current stored row and validated atomically, preserving compatible changes
- **AND** an edit that would leave both credential directories empty changes nothing and returns HTTP 400 from REST or an inline validation error from Settings
