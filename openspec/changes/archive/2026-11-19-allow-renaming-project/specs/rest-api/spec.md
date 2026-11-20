## ADDED Requirements

### Requirement: Project rename

`PUT /api/projects/{name}` SHALL accept an optional `new_name`. When present and
different from `{name}`, the daemon SHALL atomically rename the project and
rewrite all stored references to it.

#### Scenario: Rename succeeds
- **WHEN** a DB-defined project `alpha` has tasks and a per-project account and `new_name` is `beta`
- **THEN** the project, its tasks, schedules, hera roles and account mapping all reference `beta` and none reference `alpha`

#### Scenario: Target name exists
- **WHEN** `new_name` names another existing project
- **THEN** the response is 409 and nothing changes

#### Scenario: Defined in config.toml
- **WHEN** the project is defined in `config.toml`
- **THEN** the response is 409 and nothing changes
