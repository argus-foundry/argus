## Context

The resident already has a durable task inbox and idle-gated reliable notifier. The parked heartbeat design's D1 warns against expensive polling turns: these timers are purposeful promises, set by Solo or Aaron, not a gather/triage pipeline.

## Goals / non-goals

- Durable one-shot and recurring wake-ups in the singleton resident inbox.
- Preserve notes, creation time, due time and local timezone across all session lifecycles.
- No new tasks, mail/Slack ingestion, source polling or heartbeat processing.
- Named frontend follow-ups: `add-web-resident-timers` and `add-mac-resident-timers` render/manage the REST timer surface. TUI cancellation is deferred to `add-tui-resident-timer-management`; this change provides count and next due time.

## Decisions

### D1. Dedicated store and transactional fire

SQLite holds timers independently of sessions. Each fire inserts one system task-inbox message and advances/deactivates its timer in one transaction. A pending delivery record points to that message until the existing notifier reports submission or the message is acknowledged. The daemon retries after unavailable/busy sessions and notifier deadline expiry. Stable message delivery ids deduplicate repeated registration. The inbox row is exactly once; PTY submission inherits the existing notifier's acknowledgment semantics, including its unavoidable crash window between a terminal write and durable submission bookkeeping.

### D2. Purposeful bounded recurrence

Accept RFC3339 absolute times or Go durations for one-shots. Recurrence accepts five-field cron, cron descriptors and `@every` durations, with a required IANA timezone. Reject timezone overrides inside expressions. Enforce 64 active timers, 4096-byte nonblank notes, and at least 15 minutes between recurrences. Validate cron minute/hour combinations conservatively so rare calendar matches cannot hide a short interval. Compute the next recurrence strictly after now, coalescing all missed occurrences into one late fire. Nonexistent DST times are skipped; repeated local times follow the cron library's offset-aware semantics.

### D3. Existing resident identity boundary

Four MCP tools (`resident_timer_set`, `resident_timer_schedule`, `resident_timer_list`, `resident_timer_cancel`) require the authenticated caller-bound resident identity; arguments never choose a target. REST exposes a read-only list. Add count/next due to resident state and render a Solo pane title summary without adding keys or changing supervisor/BuildCmd.

## Risks / trade-offs

- A slow resident can accumulate unread fires: coalesce a recurring timer while its preceding fire is still pending, and keep the active cap small.
- Cron DST behavior is subtle: explicit timezone and transition tests pin it.
- A wake-up costs a full resident turn: orientation requires a concrete follow-up or requested recurring purpose, and reconciliation with open-loops memory.

## Migration plan

Initialize timer tables once per DB handle, on first use of the resident timer store, so ordinary task/test databases pay no additional setup cost. Reopening an existing store only checks the schema and preserves all rows. Install and bounce daemon; supervisor and resident restart are unnecessary for the tools, while fresh orientation applies on next recycle. Rollback leaves inert tables.
