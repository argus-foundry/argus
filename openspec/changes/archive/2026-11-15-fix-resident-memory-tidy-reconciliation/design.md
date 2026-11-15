## Context

This is the coordinator-approved review correction for PR #1230. The timer foundation is rejecting symbolic Local zones, so built-ins must store a concrete loadable name.

## Goals / Non-goals

Goals: track host zone changes, isolate built-in failures, and preserve pending deliveries through text updates.

Non-goals: UTC fallback, changes to timer UI controls or the resident's personal timer scheduling contract.

## Decisions

- Try a valid TZ first, then the /etc/localtime symlink's suffix after zoneinfo/. Reject empty, invalid and Local values. An unresolved zone disables only the built-in, with a log entry.
- Treat expression, timezone and enabled state as schedule identity; a note-only change updates the stored note without changing due time or an existing inbox delivery.
- Keep the existing message body for an already queued fire; updated instructions apply to future fires.
- Log a reconciliation error and continue firing and delivering personal timers. Use an injected timezone resolver for deterministic daemon error-path tests.
- Keep older explicit preferences, annotated with a dated superseded-by reference, and update links only within the owned memory folder.

## Risks / Trade-offs

- Hosts without a usable TZ or zoneinfo symlink lose the built-in → log and disable it explicitly rather than guessing UTC.
- Already queued notes retain their original text → preserve durable work rather than silently retracting it.
