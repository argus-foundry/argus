## ADDED Requirements

### Requirement: Project REST endpoints expose and preserve the diligence profile

`GET /api/projects/full`, `POST /api/projects` and `PUT /api/projects/{name}` SHALL return each project's `profile` (empty string for the default binding). `POST` SHALL accept `profile`. `PUT` SHALL preserve the stored profile when `profile` is omitted, clear it when `""`, and set it when a name. An unknown nonempty profile or blank path SHALL return 400, an unknown project on `PUT` 404, a project defined in config.toml 409, and a store failure 500. After a committed change the daemon SHALL emit `config.changed` with payload `{"resource":"projects","name":<name>}` and no task id; a no-op save SHALL emit nothing.

#### Scenario: Unrelated edit keeps the profile

- **WHEN** a project with profile `lean` is saved with only a new path and backend
- **THEN** the stored profile is still `lean` and the response says so

#### Scenario: Explicit clear

- **WHEN** `PUT` carries `"profile":""`
- **THEN** the stored profile is empty

#### Scenario: Unrelated save overlaps an explicit profile change

- **WHEN** a PUT omits profile and another writer commits an explicit profile change before that PUT writes
- **THEN** the PUT preserves the current profile atomically and emits config.changed only if its own committed row changes

#### Scenario: Identical POST

- **WHEN** POST saves an existing project with identical values
- **THEN** it returns the project without emitting config.changed

#### Scenario: File-only project ownership

- **WHEN** PUT targets a project defined only in config.toml
- **THEN** it returns 409 without creating a database project
