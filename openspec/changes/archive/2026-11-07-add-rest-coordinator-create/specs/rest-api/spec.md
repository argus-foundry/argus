## ADDED Requirements

### Requirement: Coordinator creation choices over REST

`GET /api/hera/coordinator-options?project=<name>` SHALL return 200 `{branches, profiles, archetypes, defaults:{base_branch, profile, archetype}}` listing the valid choices for the configured project, and 404 for an unknown or missing project.

#### Scenario: Choices for a configured project
- **WHEN** a client requests options for a configured project
- **THEN** the response lists remote branches with the default first, valid profiles, the canonical archetypes and the defaults

#### Scenario: Unknown project
- **WHEN** the project is not configured
- **THEN** the response is 404

### Requirement: Create a bound coordinator over REST

`POST /api/hera/orchestrators` with `{request_id, project, name, prompt, base_branch, profile, archetype, backend, model}` SHALL create an orchestrator, coordinator role, binding and task through the transactional coordinator spawn and respond 201 `{orchestrator_id, role_id, binding_id, task_id, name, base_branch, profile, archetype}`. It SHALL require API authentication, reject invalid JSON, unknown fields, an empty prompt, a non-UUID `request_id`, an unresolvable ref, an unknown profile or archetype and an unconfigured backend with 400, an unknown project with 404, and a start failure with 500 leaving no orphan task, branch, worktree, role, binding or orchestrator. Empty branch SHALL use the project default, empty profile SHALL inherit the project binding and empty archetype SHALL select `orchestrator`.

#### Scenario: Successful creation
- **WHEN** a client posts a valid body
- **THEN** the response identities match the persisted task, role and binding

#### Scenario: Start failure
- **WHEN** the session fails to start
- **THEN** the response is 500 and no row, branch or worktree remains

#### Scenario: Unknown fields refused
- **WHEN** the body carries a `command` or `worktree` field
- **THEN** the response is 400
