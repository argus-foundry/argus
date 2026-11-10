## ADDED Requirements

### Requirement: Dynamic backend selection resolves through usage tiers

The system SHALL recognize the reserved backend name `dynamic` in default, project, and explicit task backend selection as a request to choose a concrete backend using the configured backend-routing tiers. It SHALL apply the existing tier strategy, probe freshness, threshold, account, and headroom semantics. The selected task SHALL persist the concrete backend name, not `dynamic`, and subsequent launches SHALL use that backend without re-routing it.

#### Scenario: Dynamic default selects first eligible tier

- **WHEN** the configured default backend is `dynamic` and a tier is eligible
- **THEN** task creation selects that tier's concrete backend and account

#### Scenario: Explicit dynamic selection uses configured tiers

- **WHEN** a task explicitly selects `dynamic` and at least one tier is eligible
- **THEN** task creation selects and persists the eligible tier's concrete backend

#### Scenario: Dynamic project selection uses configured tiers

- **WHEN** a project's backend is `dynamic` and at least one tier is eligible
- **THEN** task creation selects and persists the eligible tier's concrete backend

#### Scenario: No eligible tier falls back to the configured concrete default

- **WHEN** `dynamic` is selected and tier resolution returns no backend
- **THEN** task creation uses the configured default backend if it names an available concrete backend

#### Scenario: No tier or concrete fallback exists

- **WHEN** `dynamic` is selected, no tier resolves, and no available concrete default exists
- **THEN** task creation fails with the existing no-backend-configured error

#### Scenario: Real backend pins remain pinned

- **WHEN** a task or project selects a concrete backend other than `dynamic`
- **THEN** usage routing SHALL NOT replace that backend

### Requirement: Dynamic backend is available from backend selectors

The system SHALL expose `dynamic` as a reserved backend option in the shared backend listing and in local TUI selectors, with a human-readable `Dynamic` label. Backend administration SHALL NOT allow creating, updating, or deleting a configured command under the reserved `dynamic` name.

#### Scenario: Clients load backend choices

- **WHEN** an authenticated client requests the backend list
- **THEN** the response includes the reserved `dynamic` option alongside configured concrete backends

#### Scenario: Reserved selector cannot become a command backend

- **WHEN** an administrator attempts to create, update, or delete a configured backend named `dynamic`
- **THEN** the operation is rejected and no configured backend is changed
