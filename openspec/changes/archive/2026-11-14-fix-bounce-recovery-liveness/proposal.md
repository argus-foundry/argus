## Why

The November 13 supervisor bounce missed live agents whose task statuses were stale, and timed-out starts lost their restart notes. Recovery must follow session liveness and preserve the resumed conversation.

## What changes

- Select recovery candidates from pre-bounce liveness and eligible live Hera bindings, retaining resident, close-out and idle-worker exclusions.
- Restore in_progress on in-place restarts, including recycle and account moves.
- Deliver owed notes when an earlier timed-out attempt actually started the live session.
- Fail recovery when a conversation cannot be resumed instead of replaying the original prompt.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `bounce-recovery`: Liveness-based recovery, owned retry note delivery and strict resume validation; restart status invariant.

## Impact

Supervisor shutdown, daemon reconciliation and recovery, client start-result tracking, recycle and account move. No REST contract or UI controls change; TUI, web and macOS observe the same corrected task status and sessions.
