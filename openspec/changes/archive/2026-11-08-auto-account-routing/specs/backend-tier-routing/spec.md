## ADDED Requirements

### Requirement: Unpinned account requests are routed and persisted as routable

A task created without an explicit or project account (a blank account, which the new-task forms submit when `default` is selected) SHALL have its account chosen by tier routing. The task SHALL be persisted with `account_routed=true`, storing the routed account name, or a blank account when routing lands on `default`. An explicit account (a configured name or `default`) SHALL remain a pin that scores every Claude tier as that account. Every account decision SHALL be logged with a `[routing]` prefix stating whether it was auto-routed or pinned.

#### Scenario: Unpinned request routes by headroom

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

Routing SHALL always score each tier's real usage reading, and SHALL add a recent-spawn penalty of 5 usage points per spawn on the same backend and account as a short-lived placeholder for usage the next reading has not yet measured. A spawn SHALL stop counting only once a reading for that account taken at least 5 minutes after the spawn is available (a fresher reading taken sooner cannot yet reflect the spawn's usage), and SHALL never count after 30 minutes (the backstop for readings without a timestamp). The penalty applies to threshold checks and headroom scoring. A spawn SHALL be recorded atomically with the resolution that produced it so concurrent spawns observe each other. Among tiers with no usable reading, the tier with the fewest recent spawns SHALL win, list order breaking ties. The penalty SHALL be deterministic given an injectable clock and SHALL be logged with a `[routing]` prefix whenever it is applied.

#### Scenario: Burst alternates between accounts

- **WHEN** headroom routing sees `default` at 40% and `personal` at 38% (threshold 80) and four spawns resolve in a row
- **THEN** the spawns do not all land on `personal`; the penalty moves later spawns to `default`

#### Scenario: Fresh reading clears the penalty

- **WHEN** a reading taken at least 5 minutes after the last spawn arrives for the account
- **THEN** that spawn adds no penalty and the real reading is scored alone

#### Scenario: Early reading does not clear the penalty

- **WHEN** a reading arrives 2 seconds after a spawn
- **THEN** the spawn still adds its penalty

#### Scenario: Penalty expires

- **WHEN** the last spawn on an account is older than 30 minutes
- **THEN** it adds no penalty

#### Scenario: Pure resolution does not reserve

- **WHEN** a caller resolves a backend without creating a task
- **THEN** no spawn is recorded

### Requirement: Unpinned account resolution order

When no account is pinned, the account SHALL be resolved in this order, first match wins: (1) the project's `project_accounts` account, if valid; (2) the best account by routing strategy among the tiers of the task's backend (the explicit backend, else the project's backend, else the best tier overall); (3) when the backend was not given explicitly and the project's backend has no eligible tier, the best tier overall across ANY backend, adopting that tier's backend and account together. An explicitly requested backend SHALL never be changed by step 3. Hera coordinator and sub-coordinator spawns always carry an explicit Claude-capable backend, so they SHALL never reach step 3 and always stay on Claude. When no step yields an account the task runs on the default account (stored blank). The deciding step SHALL be logged with a `[routing]` prefix.

#### Scenario: Project account wins

- **WHEN** `project_accounts` maps the project to `work` and `personal` has more headroom
- **THEN** the task runs on `work`

#### Scenario: Backend's best account

- **WHEN** the project has no account pin, its backend is Claude, and `personal` has more Claude headroom than `default`
- **THEN** the task runs on `personal`

#### Scenario: Project backend has no tier, best overall on any backend

- **WHEN** the project's configured backend has no eligible tier and a Codex tier has the most headroom overall
- **THEN** the task adopts that tier's backend and account

#### Scenario: Explicit backend is never changed

- **WHEN** the caller names a backend that has no eligible tier
- **THEN** step 3 is skipped and the backend is unchanged

#### Scenario: Coordinators stay on Claude

- **WHEN** a coordinator spawns while a Codex tier has the most headroom overall
- **THEN** the coordinator uses a Claude-capable backend

### Requirement: Spawns route on fresh readings, never unknown-to-first-tier

Usage readings SHALL be polled every 5 minutes. Before resolving a spawn's backend/account with a tier list configured, task creation SHALL force a fresh usage read (debounced to one probe round per 30 seconds, fail-open to cached readings on error); the spawn SHALL wait at most 5 seconds for it, even when a probe is queued behind an in-flight poll probe, and a round abandoned at the cap SHALL still count for the debounce; a TUI process SHALL obtain it through a daemon RPC and re-dial the daemon once if its connection is dead. When a tier's reading is unavailable, routing SHALL score its last known reading if it was last available to routing at most 6 hours ago, instead of treating it as unknown, and SHALL log that it did; each of that reading's quota windows is remembered with its own reset, and the recall scores the fullest window that has not reset yet. Only a tier never read, last available more than 6 hours ago, or whose remembered windows have all since reset is unknown, and unknown tiers SHALL rotate by fewest recent spawns rather than resolving to the first tier in list order every time.

#### Scenario: Spawn probes first

- **WHEN** a task is created with tiers configured
- **THEN** a fresh usage read runs before the account is resolved, and a burst of spawns within 30 seconds shares one probe round

#### Scenario: Probe failure keeps the last known reading

- **WHEN** `default` was last read at 78% and `personal` at 8%, and both probes now fail
- **THEN** the spawn routes to `personal`

#### Scenario: A reset invalidates the last known reading

- **WHEN** Codex was last read at 100% with a reset an hour later, and no Codex reading is available after that reset
- **THEN** Codex is scored as unknown, not as 100%

#### Scenario: A refilled window stops counting

- **WHEN** an account was last read with its 5-hour window at 95% and its weekly window at 85%, and its 5-hour window has since reset
- **THEN** it is scored at 85%, until the weekly window resets too

#### Scenario: Fresh read is capped

- **WHEN** the fresh read's probe is still blocked after 5 seconds
- **THEN** the spawn stops waiting and routes on the cached or last known readings

#### Scenario: Dead TUI connection

- **WHEN** the TUI's daemon connection was lost to a daemon restart
- **THEN** routing re-dials once and uses the daemon's readings instead of treating every tier as unknown
