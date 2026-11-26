## 1. Command router

- [x] Add `internal/voice` allowlist, brief speech, jobs, idempotency, and capped wait.
- [x] Refuse `kb_ingest` and `kb_delete`. Require confirm for stop. Require a task id for send.

## 2. Wire existing operations

- [x] Build the status snapshot once and use it for `GET /api/status` and the brief.
- [x] Implement the operator on `api.Server` with the existing stop, resume, and input paths.
- [x] Register `argus_brief`, `argus_commands`, and `argus_command` on the MCP server when the operator is set.

## 3. Notify-back

- [x] Add `events.Subscribe` without replacing the SSE sink.
- [x] POST needs-input and task-completed notices to a loopback `[voice] webhook_url`.

## 4. Docs and tests

- [x] Cover the router, webhook, MCP tools, and operator.
- [x] Document voice control in the README and the events gotcha.
