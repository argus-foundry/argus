## Why

The PR poller can send a completion prompt after a worker has already completed, leaving unread mail that prevents `hera_archive` forever. Daemon notices need explicit provenance so stale prompts can be retired without ignoring coordinator instructions.

## What changes

- Suppress role merge prompts for complete or archived tasks, including stale poll snapshots.
- Persist daemon merge notice provenance atomically with the message.
- Exclude only proven merge prompts from archive inbox checks and mark them read inside the archive transaction.
- Repair legacy notices only by explicit operator-attested message ID; never infer provenance from text.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `hera-auto-archive`: stale daemon merge prompts do not block completed workers.
- `pr-status`: role merge prompts skip completed tasks and carry durable provenance.

## Impact

Daemon poller, Hera message storage, archive preflight and transaction, regression tests, and orchestration gotchas. No REST schema changes; TUI, web, and macOS share the same daemon behavior.
