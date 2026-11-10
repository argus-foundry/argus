## ADDED Requirements

### Requirement: Account default endpoints

The server SHALL expose the global and per-project account defaults and let any authenticated token change them unless config.toml owns the selection.

#### Scenario: Distinguishable values

- **WHEN** a client GETs `/api/account-defaults`
- **THEN** each entry carries `explicit` (null when unset), `effective`, `source` and `editable`

#### Scenario: Set and clear

- **WHEN** a client PUTs `{"account":"work"}` then `{"account":null}`
- **THEN** the stored selection is set then removed, other projects' mappings are untouched, and `config.changed` is emitted each time

#### Scenario: Rejections

- **WHEN** the account property is missing or the account is unknown
- **THEN** the response is 400; an unknown project is 404; a config.toml-owned selection is 409; nothing is stored

#### Scenario: Existing tasks

- **WHEN** a default changes
- **THEN** tasks already bound to an account keep it
