## Why

Two gaps left open by the usage-headroom routing change (#1157): an `auto` account move pinned the task (so workers and children inherited the pick instead of routing), and burst-ledger reservations leaked when a spawn failed after reserving or when the task ran on a different account than the one charged.

## What Changes

- `task_move_account` / REST / TUI `auto` target stores `account_routed=true`; a named account or explicit `default` stays a pin.
- A failed `CreateAndStart` rolls its burst reservation back (logged with `[routing]`); an ambiguous start keeps it.
- When the routed account cannot run the backend and the task falls back to default, the reservation is moved to the account the task actually runs on.

## Capabilities

### Modified Capabilities

- `backend-tier-routing`: account-move routed flag; reservation rollback and charge-accuracy.

## Non-Goals

- No wire/REST shape change: all three frontends reach moves through `agent.MoveTaskAccount`, so parity is unchanged.
