## ADDED Requirements

### Requirement: Unpinned account requests are routed and persisted as routable

A task created without an explicit or project account (a blank account, or the alias `auto`) SHALL have its account chosen by tier routing. The task SHALL be persisted with `account_routed=true`, storing the routed account name, or a blank account when routing lands on `default`. An explicit account (a configured name or `default`) SHALL remain a pin that scores every Claude tier as that account. Every account decision SHALL be logged with a `[routing]` prefix stating whether it was auto-routed or pinned.

#### Scenario: Auto request routes by headroom

- **WHEN** a task is created with a blank account, `default` is at 70% and `personal` at 7% (both threshold 80)
- **THEN** the task runs on `personal` and is stored `account_routed=true`

#### Scenario: Implicit default is still routable

- **WHEN** routing lands on `default` for a blank-account task
- **THEN** the stored account is blank and `account_routed` is true

#### Scenario: Explicit default stays a pin

- **WHEN** a task is created with account `default`
- **THEN** it is stored as `default`, `account_routed` is false, and routing does not move it

### Requirement: Children of an unpinned parent route; children of a pinned parent inherit

Hera workers (via the orchestrator's coordinator task), MCP `task_create` callers and forks SHALL treat a parent whose stored account is blank or `account_routed` as unpinned, so the child routes. A parent with a non-blank, non-routed account SHALL pass that account as an inherited pin. No path SHALL convert a blank parent account into a `default` pin.

#### Scenario: Worker of an auto-routed coordinator routes

- **WHEN** a coordinator stored blank + routed spawns a worker
- **THEN** the worker is routed by headroom, not pinned to `default`

#### Scenario: Worker of a pinned coordinator inherits

- **WHEN** the coordinator is pinned to `personal`
- **THEN** the worker runs on `personal`

#### Scenario: Fork of a routed task routes

- **WHEN** a task stored blank + routed is forked
- **THEN** the fork's account is chosen by routing

### Requirement: Recent spawns penalize an account during routing

Routing SHALL add a recent-spawn penalty of 5 usage points per spawn on the same backend and account within the last 30 minutes to that tier's measured usage, for threshold checks and headroom scoring. A spawn SHALL be recorded atomically with the resolution that produced it so concurrent spawns observe each other. Among tiers with no usable reading, the tier with the fewest recent spawns SHALL win, list order breaking ties. The penalty SHALL be deterministic given an injectable clock and SHALL be logged with a `[routing]` prefix when it changes the outcome.

#### Scenario: Burst alternates between accounts

- **WHEN** headroom routing sees `default` at 40% and `personal` at 38% (threshold 80) and four spawns resolve in a row
- **THEN** the spawns do not all land on `personal`; the penalty moves later spawns to `default`

#### Scenario: Penalty expires

- **WHEN** the last spawn on an account is older than 30 minutes
- **THEN** it adds no penalty

#### Scenario: Pure resolution does not reserve

- **WHEN** a caller resolves a backend without creating a task
- **THEN** no spawn is recorded
