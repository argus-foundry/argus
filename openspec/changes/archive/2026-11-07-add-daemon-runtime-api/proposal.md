## Why

ENG-1054: REST clients can rebuild-and-restart but cannot restart only the daemon or inspect daemon/supervisor binary coherence.

## What changes

- Add `GET /api/runtime` (boot id plus daemon/installed/supervisor identity) and master-only `POST /api/daemon/restart` (restart-only, boot-id guarded) built on BootInfo and the shared skew policy.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `rest-api`
- `daemon-lifecycle`
- `binary-coherence`

## Non-Goals

- ENG-1103 (Web daemon restart and binary status in System) is the named Web follow-up; TUI benefits from the shared policy with no new control; native macOS adoption is a separate follow-up.

## Impact

- **Code:** `internal/api`, plus daemon/skew wiring where noted in design.
- **Frontend parity:** API-only by design; see Non-Goals.
