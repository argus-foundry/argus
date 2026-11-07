## Decisions

- **Shared service in `internal/hera`.** Takes narrow store, session and recycle-runner interfaces; `internal/daemon` supplies the real adapters. The TUI builds the same service in local mode and runs force recycle inline (`wait=true`); REST runs it in the background.
- **Receipt table is generic**: `(id, request_id UNIQUE, fingerprint, kind, subject_type, subject_id, task_id, state, error, active)` with a partial unique index on active rows per subject. The fingerprint (`kind|role_id`) detects request_id reuse for a different request (409).
- **Validate before record, replay first.** An identical `request_id` returns the stored operation before any validation, so a retry after the session died still returns the receipt and never writes twice.
- **Bounce completion** comes from `RecycleWatcher.SetOutcomeHook`: a restart for the role advances an awaiting bounce to `restarted`; a watcher error marks it `failed`. No timeout.
- **Safe errors.** Persisted/returned errors are fixed strings; details go to the log only. Handoff bodies are never logged.
- **Recovery.** `Recover()` at daemon start fails `force_recycle` operations left in `restarting` (the restart outcome is unknown). `awaiting_handoff` bounces survive because `pending_recycle` is durable.

## Deviations from the ticket

- A forced recycle of a coordinator with no live session returns 409 (as the ticket lists), even though `RecycleCoord` could start a fresh session.
- Replays return 202 with the stored operation (current state), not a distinct status.
