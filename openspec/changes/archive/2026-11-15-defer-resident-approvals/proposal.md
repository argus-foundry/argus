## Why

Solo currently stops at permission prompts until Aaron answers. Approval requests must become durable board decisions so Solo can continue other work and switch accounts while a decision waits.

## What Changes

- Intercept resident PermissionRequest hooks, persist the exact call, and deny that attempt with instructions to continue.
- Add human-only TUI approval and denial of stored calls, expiring one-shot grants, and reliable `[argus] ` decision notices.
- Preserve native ask/deny policy and suppress waiting detection during the transient hook dialog.
- Record the verified Claude Code proof and named exclusions.

## Capabilities

### New Capabilities

- `resident-approvals`: Durable deferred resident approvals and exact retry authorization.

### Modified Capabilities

- `resident-agent`: Supported outward-action and classifier-escalation prompts become board decisions instead of terminal waiting; unsupported native prompts retain existing behavior.

## Impact

Resident spawn settings and supervisor surfaces, CLI hook command, daemon RPCs, SQLite approval storage, board display/actions, resident waiting detection, documentation and tests. Web/macOS approval controls are the named follow-up `resident-approval-client-parity`.
