## Context

The board stores decisions and FYIs in `hera_board_items`, whose kind CHECK permits only those values. Decision options occupy the existing `structured` JSON. Read acknowledgments, revision checks, response receipts and durable notice delivery already protect human interactions. The new kinds must preserve those guarantees and avoid treating team work as an ask for Aaron.

Read-only inspection of the supplied live row IDs confirms human commands, human UI steps and future team work. The native macOS app has no board models or board rendering today.

## Goals / Non-Goals

**Goals:**

- Represent human steps and future team work without altering decisions or FYIs.
- Make blocking actions immediately prominent and pending triggers quiet.
- Persist completion outcomes, metadata and revision checks across every existing board surface.

**Non-Goals:**

- Historical item reclassification: a separate, reviewed one-off is a follow-up.
- Automatic trigger evaluation, scheduling, spawning or command execution: coordinators explicitly mark readiness.
- Things 3 synchronization: the board kind `todo` is separate from `internal/todo` and `todo_*`; evaluate optional sync in a future change.
- Native macOS board UI: named follow-up **macOS coordinator-board parity**, including all four kinds, metadata and completion controls. There is no native board in this branch to extend.

## Decisions

### Typed columns for operational fields

Add top-level stored/wire fields `blocking` (boolean, default false), `trigger` (optional plain text, empty default), `trigger_pending` (boolean, default false) and `command` (optional plain text, empty default). These fields are restricted to `action`/`todo`; `blocking` and `command` belong only to `action`. The command can be an exact shell command or a human UI step, never an executable instruction to the app. New action posts must explicitly supply `blocking` so an omitted value cannot silently hide a blocker. A pending trigger requires nonempty trigger text and cannot coexist with `blocking=true`. A deferred action is `blocking=false`; it can remain nonblocking after its trigger becomes ready.

Use dedicated columns because sorting, unread attention queries and archive safety need explicit data. Keep `structured` exclusively for the existing decision options; reject it on the new kinds. Reject whitespace-only supplied trigger/command and cap trigger at 1024 UTF-8 bytes and command at 16 KiB. Clearing uses an empty string. Updates preserve omitted metadata; kind changes clear metadata invalid for the new kind. Named MCP decode structs and advertised schema parity tests cover all fields. List accepts all four kind values as an optional filter.

Alternative: extend `structured`. Rejected because decision validation currently requires options, and operational predicates would become JSON-shape dependent.

### Explicit trigger readiness and quiet attention

`trigger` is descriptive text such as “after 1.42 ships”; `trigger_pending` is the explicit gate. A post with a nonempty trigger defaults to pending unless readiness is supplied. A post without a trigger is ready. Coordinators set `trigger_pending=false` when they verify the condition, using `hera_board_update`. No calendar parser or automatic external watcher is introduced.

Pending items stay visible in their normal board and kind tabs with a Deferred label and trigger. They retain read state but contribute zero to mailbox/attention/unread counters, landing and needs-input jumps. Ready board todos also contribute zero human-attention count: they are team work and Aaron does nothing now. The coordinator still sees every open item and its readiness through list and recovery summaries. Readiness and metadata changes are material edits. A deferred action becoming blocking is explicit and raises attention.

Alternative: only display trigger text. Rejected because that would keep future work ringing Aaron's mailbox.

### Stable priority order

Use one shared order for store/API/TUI/web: unresolved blocking actions first; existing decisions/FYIs and ready nonblocking actions next; pending-trigger items and board todos last. Within each band preserve the board's existing ascending numeric-id order. Kind-filtered views retain the same relative order. Blocking priority outranks read state and freshness. Existing decisions and FYIs retain their relative ordering and semantics.

Alternative: a fixed four-kind sort. Rejected because it would reorder existing decision/FYI relationships and conflate urgency with ownership.

### Durable completion with existing response safeguards

An action completes with `done`, optional note; it is never a decision answer or an FYI dismissal. A board todo completes with `done` or `dropped`, optional note. Store the outcome in `resolution_choice` alongside existing resolver/note fields, without changing decision choice values. Coordinator agent resolution defaults to done for the new kinds and may explicitly choose dropped for board todos. Human resolution requires the expected revision; repeat action IDs replay the recorded receipt. A mismatched kind/outcome is invalid.

Completion and its coordinator notice commit atomically using the existing board-notice outbox. Both done and dropped notices say which outcome occurred. Command contents are not written to logs. New human operations retain per-item conflict/invalid results, idempotent retry and uncertain-submit recovery. Existing decision/FYI operations keep their current behavior.

The TUI adds rebindable `d` for Done on either new kind and uses existing `x` for Dropped on board todos; `x` on an action does not silently dismiss it. A direct Done needs no note; optional feedback can be entered through the shared browser completion control. Actions show the exact step with a dedicated copy key/control; no command executes. Update generated help assertions and Reference keybindings.

Web board and the shared rail/page expose Actions and Todos alongside Decisions and FYIs. Every new card shows readiness/trigger and action blocking/command metadata; commands have Copy. Completion buttons require no note, with an optional note box. New operations use the existing global queue, review, guarded drafts, receipt reconciliation and Submit/Submit & close controls. Existing Decisions/FYIs keyboard shortcuts remain unchanged; new tabs are reachable through visible controls, normal keyboard focus and adjacent-tab swipe.

### Archive safety

Normal sub-coordinator archive refuses any open action or board todo, pending or ready, just as an open decision prevents archive. Existing FYI forwarding remains unchanged. Coordinators must settle the item or explicitly move/repost responsibility before archive; automatic copying of these obligations is out of scope. Preserve the established explicit force-with-reason escape hatch. Apply checks in both the fast verdict and transactional re-verification.

## Risks / Trade-offs

- [Table rebuild can cascade-delete dependent approvals/receipts] → Rebuild with foreign keys disabled outside the transaction, copy every column/id, recreate indexes/triggers, verify foreign-key integrity, then restore enforcement. Add migration fixtures with dependent rows and verify repeat opens.
- [Pending items may stay pending after their trigger] → Show the explicit state in coordinator recovery/list guidance; the coordinator owns verifying readiness.
- [Kind checks scattered across clients omit new items] → Cover store, API roster/counts, launch focus, shared rail/page, TUI navigation and archive in targeted tests.
- [Completion could be mistaken for permission to execute] → Done records Aaron's completion only; never execute or infer it from read/acknowledgment.
- [The board todo name collides with Things tools] → Use identifiers such as `boardTodo`/`HeraBoardTodo` and explicitly distinguish docs and guidance.

## Migration plan

Add an idempotent schema migration following the repository's SQLite rebuild patterns. Preserve every existing row, ID, timestamp, read/resolution value, structured decision and dependent record; add only neutral metadata defaults and widen the kind CHECK. No live data writes during development. Test foreign-key enforcement after migration and repeated open. Deploy with the normal daemon update; rollback requires reverting newly created action/todo rows before an older binary can safely own the DB, so prefer a forward fix.

Archive this change into base specs in the same PR after implementation and validation. Run targeted package tests during implementation and the complete `make pre-pr` gate before pushing via iris or creating the PR.

## Open questions

The coordinator must approve this proposed field shape, readiness/attention policy, priority bands and completion/notice semantics before implementation. Both new kind names are already approved.
