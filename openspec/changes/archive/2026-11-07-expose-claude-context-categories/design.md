## Decisions

- **Producer mapping is by row label.** The engine types breakdown rows only as `used`/`free`/`buffer`/`deferred` and documents the label as the display name, so the mod maps labels (`System prompt`, `System tools` + `MCP tools`, `Custom agents`, `Memory files`, `Skills`, `Messages`) and the `buffer` row (compaction reserve). Unknown labels and deferred rows are dropped, never guessed. Counts are the engine's own estimates of each row, not role totals inferred by Argus.
- **Deviation from the ticket wording:** the ticket calls categories "measured"; the engine documents them as estimates (a `summary` breakdown estimates throughout). Argus reports them as the engine's measurement and does not recompute.
- **`summary` detail, not `full`:** `full` calls the token-count API per category on every measurement; too costly for a message that fires after each tool step.
- **Breakdown only when the context unit changed**, and any failure sends the measurement without categories (fail open).
- **Stale vs replaced:** a stale (non-live) recorded measurement is returned with `live=false`. A measurement recorded under a different launch generation than the task's running process is returned as `context=null`. The hub already clears the snapshot when the session id or generation changes.
- **Invalid measurement = 400 for the whole batch**, validated before any event is applied. The mod drops 4xx batches without retry, so this cannot wedge its queue.
- **Event is an invitation only**, emitted after a live measurement carrying context.
- The response is provider-neutral (`provider`, `source`) so Codex context can fill it later.
