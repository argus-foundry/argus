# Archive pending reconciliation

## Why
Archive stream history has no replay boundary. Treat every archive envelope as unresolved until a later task read succeeds.

## What Changes
Block input immediately in primary and split terminals, keep the row in place with an Archive pending label, and clear pending only from a task read started after that envelope. Preserve the snapshot fence for other events.

## Non-Goals
No Go, daemon, REST, original web SPA, TUI or macOS changes. Pure stream outages without an archive envelope do not fence input. Named backend follow-up `reject-archived-task-input` makes daemon handleWriteInput reject archived tasks.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| Archive pending | Value | Per-task unresolved archive revision | Archive envelopes |
| Task reconciliation | Operation | Clear only revisions observed before the read | Authoritative task snapshot |
| Input eligibility | Policy | Reject pending and archived tasks in either terminal | Task state |
| Pending indication | Visual Component | Subtle fixed row status | Archive pending |
