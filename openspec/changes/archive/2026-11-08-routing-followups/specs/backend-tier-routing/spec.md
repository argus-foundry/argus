## ADDED Requirements

### Requirement: Auto account moves leave the task routable

Moving a task's account with the `auto` target SHALL store the picked account with `account_routed=true`, so workers and children of the task route rather than inherit the pick. A move to a named account or explicit `default` SHALL store `account_routed=false` (a pin). This applies to every move entry point (REST, MCP, TUI) because they share one implementation.

#### Scenario: Auto move stays routable

- **WHEN** a task is moved with target `auto` and the picker chooses `work`
- **THEN** the task is stored on `work` with `account_routed=true`

#### Scenario: Named move pins

- **WHEN** a task is moved to `work` or to `default` by name
- **THEN** the task is stored with `account_routed=false`

### Requirement: Burst reservations track the spawn that made them

A burst reservation SHALL be rolled back when the spawn fails after reserving (including account validation, worktree, persistence and session-start failures), except when the session start is ambiguous and may still complete. When the task runs on a different account than the routed choice (for example the routed account cannot run the backend and the task falls back to `default`), the reservation SHALL be charged to the account the task actually runs on. Rollbacks and moves SHALL be logged with a `[routing]` prefix.

#### Scenario: Failed start releases the reservation

- **WHEN** a routed spawn reserves `work` and the session start fails
- **THEN** the ledger holds no reservation for `work`

#### Scenario: Fallback charges the real account

- **WHEN** routing picks an account that cannot run the backend and the task falls back to `default`
- **THEN** the reservation is charged to `default`, not to the picked account
