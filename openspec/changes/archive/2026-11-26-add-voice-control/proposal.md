## Why

Darren talks to Lumi over live voice. Lumi reaches Argus with short curl JSON-RPC turns to the local MCP server and has no worktree, so the tools that resolve a caller from `cwd` cannot answer. Voice replies have to be short plain sentences. Lumi also needs to hear when a session starts waiting or a task finishes, without holding an SSE connection open.

## What Changes

- A voice command router on the MCP server (`argus_brief`, `argus_commands`, `argus_command`) that maps a small allowlist onto the existing status, create, stop, resume, and session-input paths. Calls do not need a working directory.
- Idempotency keys so a retried voice turn returns the same job. Stop requires `confirm`. `kb_ingest` and `kb_delete` are refused on this path.
- Accepted start, stop, resume, and send calls return a job handle and one spoken sentence immediately. `wait` and `status` poll that job and say `Still running.` instead of blocking past three seconds.
- An optional `[voice] webhook_url` that POSTs a one-sentence JSON body to a loopback URL on `session.needs_input` (entering) and `task.completed`.
- `events.Subscribe` so that webhook can listen without replacing the daemon's SSE sink.

## Capabilities

### New Capabilities

- `voice-control`: spoken brief, named commands, jobs, idempotency, and the loopback webhook.

### Modified Capabilities

- `events-bus`: `Emit` also notifies subscribed listeners, including when no sink is installed. A listener panic does not stop the sink.

## Impact

- MCP tools appear when the knowledge base (MCP server) and the REST API (task operations) are both enabled.
- `GET /api/status` keeps its response shape. Its counts are computed by the same snapshot the brief uses.
- TUI, web, and macOS do not gain a voice screen. Lumi is the client. Webhook settings stay in `config.toml` (named follow-up `add-voice-webhook-settings` if a Settings row is wanted later).
- No new REST endpoint, so the three frontends do not need a contract change.
