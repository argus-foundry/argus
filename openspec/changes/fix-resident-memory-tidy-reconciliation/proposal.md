## Why

Review found that storing the host's symbolic Local zone can stop timer delivery, and instruction-only changes discard queued tidy work. Reconciliation must preserve personal follow-ups and queued work while tracking the host's actual zone.

## What changes

- Resolve a concrete host timezone from valid TZ, then the zoneinfo suffix of /etc/localtime; disable tidy and log when resolution fails.
- Continue personal delivery after built-in reconciliation errors.
- Preserve due time and queued fires for note-only updates; write zero due time on disable.
- Limit reference edits to the memory folder and preserve older conflicting preferences with dated supersession notes.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `resident-memory-tidy`: Concrete host timezone, isolated reconciliation failures and non-destructive note refreshes.

## Impact

Daemon timezone resolution, resident timer store, curation instructions and regression tests. No new frontend controls or API fields; the behavior applies across all clients.
