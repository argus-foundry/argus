## Decisions

- **Totals vs turns.** Totals come from the cumulative `total_token_usage` of the latest `token_count`, never a sum of per-call usage. Turn spend sums `last_token_usage` per call so a tail window needs no baseline; a turn whose `task_started` precedes the window is skipped, and rows stored by earlier ingests persist.
- **`input_tokens` includes `cached_input_tokens`** (Codex reports cached as a subset); clients must not add them.
- **Ordering and cursor.** Turns are newest first by start time then turn id. `next_cursor` is an opaque base64url of the last returned position; invalid values are 400.
- **Ownership.** A requested `session_id` is served only if a stored row exists for that task or it equals the task's recorded `SessionID`; otherwise 404. Rows are keyed by task, so another task's session is never visible.
- **Failure safety.** A reader error is logged and the stored counters are served unchanged; a store failure is 500. An observation older than the stored one never overwrites it.
- **Deviation.** The ticket asks for supported providers generally; only Codex has a reader. Claude returns `totals=null, turns=[]` (`provider=anthropic`, `source=bridge`).
