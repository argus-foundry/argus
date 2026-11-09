# Composer data available without daemon changes

Checked against this branch's Go handlers and wire models on November 8, 2026. No live daemon, credentials, provider API or local transcript was read. Theia uses authenticated existing GET endpoints only.

| Field | Claude | Codex | Missing daemon work |
| --- | --- | --- | --- |
| Backend and explicit task model | Raw task record | Raw task record | A dedicated safe runtime model-switch endpoint; generic raw record overwrite is deliberately unused. An omitted explicit model is not replaced by a guessed running model. |
| Context used, window and percent | /api/tasks/{id}/context measured values, scoped to the session. Live is process authority, not measurement age. | GET /context reports current rollout used/window/percent with provider openai and source codex_rollout. | Codex category breakdown remains unmeasured. |
| Recorded context | Task context with live=false retains measured_at. Older daemons use Hera metadata context_size and context_percent, pairing only equal timestamps. | GET /context with live=false displays recorded rollout measurements. Legacy metadata is used only on unsupported routes. | Legacy metadata lacks window and backend/session identity, so it supplies no inferred window or live claim. |
| Claude category breakdown | Task context categories expose measured messages, memory, tools, agents, skills, system and compaction counts. Null categories are omitted; measured zero is retained. | Not applicable | Producer availability controls which categories are measured. Role totals cannot supply these categories. |
| Input, cached input, cache writes and output | Measured totals from /api/hera appear in task Info with its cost display rules and coordinator binding precedence. They cover binding history and are absent from the composer. | Same generic REST fields can be displayed in task Info when measured. Existing collection is not a Codex context breakdown. | GET /usage provides current session and turn measurements separately in the composer; never use historical role sums as current context. |
| 5h and 7d remaining and reset | /api/accounts valid matching-account usage windows, with live task bridge rate_limits as fallback. Expired or invalid readings omitted. | Backendtier has an internal cached Codex reading, but no REST quota projection was found. Claude account readings must never be used for Codex. | Expose Codex short and weekly quota windows and reset times through REST. |
| Account (task Info only) | Raw task account. Empty account in a loaded raw record means default. The composer uses the account to match usage and Claude details but omits the Account row. | Raw task account in Info; omitted from the composer. | Actual Codex signed-in account identity. |
| Plan, email and organization | Matching account fields from /api/accounts, only when backend kind is Claude. | /api/accounts fields describe Claude identity even on shared accounts, so omitted. | Codex plan/email/provider identity projection. |
| Provider | Task context identifies anthropic as its measurement provider. The popover does not use it as account identity. | Task context identifies openai as its measurement provider. | Provider-specific account identity remains separate from context measurements. |

The context ring uses only measured context percentage. It never substitutes account quota, role token totals or Hera's configured recycle budget. Dashed means unknown, which includes a token count with no window or percent. The popover retains unsupported rows as absent, with a plain unknown-context message when needed. Task context shows measurement age and timestamp. A live measurement stays live regardless of age. Non-live measurements are labeled Last recorded. Theia coalesces same-session task.context_changed bursts for 100 ms and refreshes context independently of account usage. Valid same-session categories remain visible until the read settles. Popover open, visibility return and the foreground 15 second cadence refresh context and auxiliary usage, sharing overlapping reads. Hera metadata is read only for the legacy fallback. Shared reads have a 10 second deadline so polling can recover from hung requests. Session lifecycle events also cancel and refresh context even while task snapshots are buffered. A 404 or HTML 200 from an older daemon retains bridge/meta behavior. Supported empty responses and other failures show unavailable context. Task/session changes and disconnect reject obsolete reads.

## Source map

- `internal/api/task_context.go` and `openspec/specs/rest-api/spec.md`. Nullable task context contract, process authority and context invalidation events.
- `internal/api/handlers.go`, taskJSON and backendJSON. Task DTO omits model/account; backend roster includes server-resolved kind, avoiding custom backend-name guesses.
- `internal/api/raw.go`, handleGetTaskRaw, and `internal/model/task.go`. Existing authenticated raw task fields use lowercase JSON keys.
- `internal/api/bridge.go`, handleBridgeState, bridgeState and stampBridgeMeasurement. Live authority is process/generation checked; a diagnostic stale response has live=false. Context size/percent are persisted to Hera metadata.
- `internal/bridge/event.go`, Context and RateLimit. Context has optional tokens/percent/window, with no category or cached-token breakdown.
- `internal/api/task_meta.go`, handleGetMeta. Namespace-scoped entries expose value and updated_at.
- `internal/api/accounts.go`, accountJSON and usageJSON. Response is an array. Identity/usage refer to Claude; Codex exposes only its login boolean. LiveWindows filters expired usage.
- `internal/api/hera.go`, heraRoleJSON, and `internal/db/hera_cost.go`, SumHeraRoleRawTokensByOrchestrator. Role totals sum all historical bindings, not just the current task session.
- `cmd/argus/coord_hook.go`, readContextSizeReal. Hook stamps use the last main-chain assistant input count and do not supply a window limit.
- `internal/backendtier/usagesource.go` and `internal/daemon/rpc.go`, Usage/AccountUsage. Internal routing readings are not automatically REST data.

## Session usage adoption

The popover reads current session totals and newest-first turn pages from GET /api/tasks/{id}/usage. Cached input is included in input. Null counters display Unavailable; measured zero stays zero. Older pages include the returned session_id and before cursor. Display is capped at 500 turns. task.usage_changed and session lifecycle events refresh or invalidate the captured session. Historical binding totals remain in Info. Codex context uses the same measured GET /context projection and does not borrow Claude quotas.
