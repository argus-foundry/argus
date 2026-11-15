## Why

Solo needs purposeful wake-ups for promised follow-ups and recurring work without creating new tasks or polling external inboxes. Durable timers let the resident check its open loops at the right time using its existing tools.

## What changes

- Add durable one-shot and recurring resident timers, with bounded notes, count and recurrence frequency.
- Deliver timer fires once through the resident's idle-gated inbox delivery, including late fires after downtime.
- Add resident-only MCP timer tools, a human REST listing and a Solo TUI summary.
- Teach Solo to reconcile timers with its open-loops memory.

## Capabilities

### New capabilities

- `resident-timers`: Durable follow-ups, recurrence, delivery and resident-only management.

### Modified capabilities

- `resident-agent`: Orientation teaches follow-up and open-loop reconciliation.
- `resident-tui`: The Solo tab displays timer count and next due time.

## Impact

SQLite, resident daemon loop, MCP, REST, TUI and resident orientation. Reuses the existing cron dependency and notifier; creates no tasks or sessions. No mail/Slack ingestion or heartbeat gather/triage pipeline.
