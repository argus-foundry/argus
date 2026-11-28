## Context

The MCP server already creates, stops, and lists tasks, but several tools resolve the caller from a worktree `cwd`. Resume and session input live on the REST server, which imports the MCP package, so the MCP package cannot call those handlers directly. `events.Emit` has a single sink, installed by the API server for the SSE ring. A voice client uses short HTTP JSON-RPC turns and must not hold `GET /mcp` open.

## Goals / Non-Goals

**Goals:**

- One allowlisted command router with explicit argument schemas, callable with a task id or a unique name.
- A brief that reuses the status snapshot and adds needs-input, blocked, and recently-finished buckets.
- Job handles, idempotency, a confirm gate on stop, and a capped wait.
- Best-effort loopback notify-back that cannot block `Emit` and cannot be pointed at a public host.

**Non-Goals:**

- A voice UI on the TUI, web app, or macOS app.
- Exposing `kb_ingest` or `kb_delete` through the voice router.
- Replacing the SSE event stream. The webhook is an additional consumer.
- Settings UI for the webhook URL.

## Decisions

1. **Router in `internal/voice`, operator implemented on `api.Server`.** The API server already owns stop, resume (`startResumedSession`), input, and status. MCP receives a `*voice.Router` the same way it receives `ReadOnlyGet`, after the API server exists. That avoids an `mcp` → `api` import cycle.
2. **Commands are data plus a switch, not a free-form tool proxy.** The router never dispatches to an arbitrary MCP tool name. `kb_ingest` and `kb_delete` are explicit refusals.
3. **Jobs are in-memory.** A voice retry happens within one daemon process. The key stores the command fingerprint. Confirm refusals are not stored. Cap 256, dropping finished jobs first.
4. **`wait` caps at 3 seconds** and checks the job before sleeping, so a finished job returns immediately and a zero timeout returns `Still running.` without blocking.
5. **Listeners are a second consumer on `Emit`, not a second sink.** The SSE sink stays the one installed sink. The webhook only enqueues. Dial is forced through a loopback check and an empty proxy function so `HTTP_PROXY` cannot redirect it.
6. **Status counts stay on one function.** `handleStatus` reads `voiceSnapshot` so the spoken counts and `/api/status` cannot drift. The HTTP body is unchanged.

## Risks / Trade-offs

- Voice tools are absent unless both `kb.enabled` and `api.enabled` are on. The webhook itself only needs the daemon and a URL. Documented in the README.
- Start and resume run in a goroutine tied to MCP shutdown, not to the HTTP request. A daemon bounce cancels in-flight starts.
- Name lookup is case-insensitive and skips the resident row. An explicit task id can still address the resident.
- `[voice]` is config.toml only, read live via the existing file overlay.
