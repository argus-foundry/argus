# Composer data available without daemon changes

Checked against this branch's Go handlers and wire models on November 6, 2026. No live daemon, credentials, provider API or local transcript was read. The optional workspace uses authenticated existing GET endpoints only.

| Field | Claude | Codex | Missing daemon work |
| --- | --- | --- | --- |
| Backend and explicit task model | Raw task record | Raw task record | A dedicated safe runtime model-switch endpoint; generic raw record overwrite is deliberately unused. An omitted explicit model is not replaced by a guessed running model. |
| Context used, window and percent | Live bridge context when the mod reports it. Tokens/window can supply the percentage if only those are present. | Same generic bridge shape could represent it, but current bridge authority requires a bridge-capable family and Codex has no such producer. | Codex context producer and REST projection of current rollout measurements. |
| Recorded context | Hera metadata context_size and context_percent, with record time. A percent is paired with a size only when their timestamps match. | Only if that task has these actual metadata stamps; not assumed to exist. | Context window size and backend/session identity on persisted context stamps. Without these, no inferred window or live claim. |
| Claude category breakdown | Not exposed | Not applicable | Measured message, memory, tool, agent, skill, system and compaction category counts. Role totals cannot supply these categories. |
| Input, cached input and output | Available only when a single live task role has measured totals in /api/hera. Labeled role token totals across binding history. | Same generic REST fields can be displayed when measured. Existing collection is not a Codex context breakdown. | Per-session/per-turn token measurements for parity with the reference; never use historical role sums as current context. |
| 5h and 7d remaining and reset | /api/accounts valid matching-account usage windows, with live task bridge rate_limits as fallback. Expired or invalid readings omitted. | Backendtier has an internal cached Codex reading, but no REST quota projection was found. Claude account readings must never be used for Codex. | Expose Codex short and weekly quota windows and reset times through REST. |
| Account | Raw task account plus matching /api/accounts label. Empty account in a loaded raw record means default. | Same configured account label. | Actual Codex signed-in account identity. |
| Plan, email and organization | Matching account fields from /api/accounts, only when backend kind is Claude. | /api/accounts fields describe Claude identity even on shared accounts, so omitted. | Codex plan/email/provider identity projection. |
| Provider | No explicit provider identity in these endpoints | No explicit provider identity | Expose provider identity rather than infer it from a command name. |

The context ring uses only measured context percentage. It never substitutes account quota, role token totals or Hera's configured recycle budget. Dashed means unknown, which includes a token count with no window or percent. The popover retains unsupported rows as absent, with a plain unknown-context message when needed. Recorded context is explicitly labeled with its timestamp.

## Source map

- `internal/api/handlers.go`, taskJSON and backendJSON. Task DTO omits model/account; backend roster includes server-resolved kind, avoiding custom backend-name guesses.
- `internal/api/raw.go`, handleGetTaskRaw, and `internal/model/task.go`. Existing authenticated raw task fields use lowercase JSON keys.
- `internal/api/bridge.go`, handleBridgeState, bridgeState and stampBridgeMeasurement. Live authority is process/generation checked; a diagnostic stale response has live=false. Context size/percent are persisted to Hera metadata.
- `internal/bridge/event.go`, Context and RateLimit. Context has optional tokens/percent/window, with no category or cached-token breakdown.
- `internal/api/task_meta.go`, handleGetMeta. Namespace-scoped entries expose value and updated_at.
- `internal/api/accounts.go`, accountJSON and usageJSON. Response is an array. Identity/usage refer to Claude; Codex exposes only its login boolean. LiveWindows filters expired usage.
- `internal/api/hera.go`, heraRoleJSON, and `internal/db/hera_cost.go`, SumHeraRoleRawTokensByOrchestrator. Role totals sum all historical bindings, not just the current task session.
- `cmd/argus/coord_hook.go`, readContextSizeReal. Hook stamps use the last main-chain assistant input count and do not supply a window limit.
- `internal/backendtier/usagesource.go` and `internal/daemon/rpc.go`, Usage/AccountUsage. Internal routing readings are not automatically REST data.
