## Why

Review found failure paths that can block unrelated timers, leave orphaned delivery receipts, or stack unread recurring fires. The timer lifecycle needs bounded retention and explicit timezone validation before shipping.

## What changes

- Isolate broken recurrence calculations and continue delivery retries.
- Recover orphaned receipts and coalesce until the prior inbox event is acknowledged.
- Reject the implicit Local timezone, emit normal message events, prune completed history after 30 days, and log state-read failures.
- Correct singular timer display, strengthen delivery tests, isolate daemon-only state types, and remove unrelated CI/batching tooling from this PR.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `resident-timers`: Recovery, unread coalescing, explicit timezone and retention.

## Impact

Resident SQLite store, daemon delivery loop, event bus, and tests. Existing web/macOS timer-view follow-ups remain unchanged; no new REST surface is added.
