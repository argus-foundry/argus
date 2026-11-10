# Design

- Revision: hash of backend, model, sandbox_override, prompt and session_id; status is excluded so an unrelated status flip does not fail If-Match.
- Admission: decode the bounded whole body before the cancellable task lock, then check current revision, fields and session state. Conversation discovery remains read only until the conditional runtime commit. Rejections leave stored runtime and conversation identity untouched.
- Restart of a live session persists then `KickRerender`s. Kick errors, failed outcome probes and restarts still pending at the response deadline retain settings and report `restart_unknown`. Only checked absence of a pending restart and live session reports `restart_failed`; a confirmed live replacement reports success.
- Deviation: the ticket's 500 body is extended with `code`, `live`, `rolled_back` and `detail`; a no-op patch returns 200 without writes, events or restart.
- Deviation: `task.runtime_changed` is emitted once after the outcome is known (with `started`), not once at persist and again after restart.
- Model validation is family-based only (`ModelNone` families reject a model; id must be a plain identifier of at most 200 chars).
