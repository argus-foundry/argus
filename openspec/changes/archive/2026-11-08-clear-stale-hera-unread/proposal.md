## Why

The Claude bridge band shows e.g. "40 unread from coord" for Hera mail the agent already received. `hera_messages.read_at` is stamped only by `hera_inbox` / `hera_get_messages` / `hera_mark_read`; the doorbell carries only the tldr, so agents routinely act without fetching the body and the mail stays unread. Live (2026-11-08): 3582 delivered-but-unread messages, 3699 unread in total on roles with no live binding (1283 roles whose last binding ended `user_deleted`). Nothing ever clears mail for a role that is permanently gone.

## What Changes

- The doorbell line gains a fetch hint: `[hera from <role>] msg #<id> — <tldr> · read: hera_inbox`. The `[hera from ` prefix is unchanged (the notifier matches on it).
- Mail addressed to a role is marked read when the role's binding ends for a TERMINAL reason (`user_deleted`, `argus_deleted`, `task_missing`, `moved`) and the role has no other live binding, so it cannot re-accumulate.
- An idempotent startup sweep marks read all unread mail for roles with no live binding that are nuked or whose latest binding ended terminally. Roles never bound (planned nodes) and roles whose binding ended non-terminally (`hera_rebind`, `reparented`, spawn-materialize failures) keep their mail, because role-addressed mail waits for a later binding.
- Band semantics are NOT changed: it still counts every unread hera message for live bindings.

## Capabilities

### New Capabilities

### Modified Capabilities
- `hera-messaging`: doorbell line fetch hint; mail cleared on terminal binding end; startup sweep for dead roles.

## Impact

`internal/hera/service.go`, `internal/db/hera.go` (binding-end paths), `internal/db/hera_messages.go`, `internal/db/migrate.go`, `internal/db/tasks.go` (task delete cascade). No frontend change: the doorbell text is agent-facing, and the band keeps reading the same endpoint with a smaller, truer count. **Frontend parity (TUI, web, macOS):** the unread counts all derive from the daemon's hera inbox data, so all three get the cleaned counts with no client change; no named gaps. Daemon-only: no bridge mod change, no web shell change.
