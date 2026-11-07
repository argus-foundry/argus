## Decisions

- **Cursor space.** Frame cursors are offsets in the session handle's byte space (`TotalWritten`), the same space `AddWriterFrom` uses. They are not on-disk session-log offsets; for supervisor-proxied sessions the two differ. A subscription `since` is in handle space. `from` is inclusive and `to` exclusive.
- **Reading offsets.** Handles gain an optional `AddWriterFromAt` that attaches like `AddWriterFrom` and returns the offset of the first byte the writer will see, so frames carry exact cursors and ring eviction is detectable (`start > since` emits `gap`). Handles without it yield a per-subscription `unsupported` error.
- **Session identity.** `session_id` is an opaque per-handle id minted by the server (stable for the life of that handle). A subscription whose `session_id` differs from the live one gets `reset` and replays the new session from its ring start. An empty `session_id` accepts the current session.
- **Backpressure.** Each subscription queues at most 1 MiB. On overflow the writer is detached, queued bytes are dropped, and the subscription re-attaches at its next expected offset, emitting `gap` when the ring no longer holds it. The PTY reader is never blocked.
- **Lifecycle.** A stream allows one connection; disconnect, DELETE or 60s without a connection removes it and detaches all writers. PUT replaces the full subscription set atomically using the supplied cursors.
- **Principal.** `X-Argus-Auth` plus `X-Argus-Token-Id`; a different principal gets 403.
- **Deviation.** The ticket's 413 applies to over 64 subscriptions; 400 covers malformed or duplicate entries.
