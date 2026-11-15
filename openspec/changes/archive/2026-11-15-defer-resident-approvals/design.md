## Context

The coordinator approved this design after the research gate. Official [hook docs](https://code.claude.com/docs/en/hooks#defer-a-tool-call-for-later) restrict native defer to single-tool noninteractive runs. Solo uses an interactive PTY. A PreToolUse allow cannot bypass native ask rules; PermissionRequest answers the native permission boundary instead.

On 2026-11-14 the coordinator ran `context/spikes/resident-defer-proof.py` with Claude Code 2.1.325 on an authenticated account: all four cases passed in default and auto mode (eight total). Print and interactive ask+allow each produced one PermissionRequest and one PostToolUse; deny with interrupt:false produced one PermissionRequest, zero execution, and a continued model response. Interactive dialogs briefly rendered before the hook answered. The harness requires a trusted PROOF_BASE for interactive runs; no-session-persistence is print-only.

## Goals / Non-Goals

**Goals:**

- Solo keeps working while human approval waits, including ordinary quiet account switching.
- Only the exact stored call can receive one expiring human grant; native deny rules stay unchanged.
- Durable decisions and notices survive daemon restarts, with no authorization from agent board mutations.
- TUI human approve/deny and regression coverage for the transient native dialog.

**Non-Goals:**

- `resident-approval-client-parity`: web and macOS approval controls are deferred; they retain their current resident/board surfaces.
- `resident-hook-exclusion-parity`: native sandbox network prompts, MCP requiresUserInteraction and organization-required connector consent remain native prompts. PermissionRequest does not cover every such path.
- Replacing the resident PTY with an SDK runner, automatically reversing classifier denials, or executing deferred tools directly in the daemon.

## Decisions

- Install a resident-only PermissionRequest command hook in the isolated inline settings. A private launch file authenticates its daemon RPC, with a 64 KiB event limit, a 48 KiB canonical input/server limit and fail-closed errors. Keep the native allow/ask/deny arrays and return neither updatedPermissions nor updatedInput.
- At the hook boundary, store canonical JSON (preserve numeric precision, reject malformed/duplicate keys), tool name, cwd, session and launch identity. Hash every execution field, excluding only Bash description. Deduplicate identical pending attempts. A native deny prevents reaching PermissionRequest at all.
- A new approval row links an immutable decision on the standing-team board. TUI renders stored tool/input and explicit Approve once / Deny controls. Generic board edits/resolution never create grants; approval items cannot be rewritten into a different request.
- The human action uses revision/state checks. Approval has a ten-minute expiry and consumes atomically at the next matching PermissionRequest; denial never executes. Both write a reliable `[argus] ` notice in the same database transaction. A grant is authorization to attempt execution once, not evidence the tool completed.
- Launch rotation invalidates approved grants; pending decisions remain visible but need a new matching request on the new launch before approval. Account, session, cwd or effective-policy changes therefore cannot silently reuse a grant.
- Keep approval attention separate from ResidentWaiting. Debounce the specific rendered chooser for ten seconds; extend to fifteen from its original start only when its displayed command/tool matches the answered call. Unrelated RPCs cannot start or refresh this deadline; a changed chooser surfaces immediately, and generic proceed prose is never a permission marker.
- Match only Bash and MCP PermissionRequest events in settings and the binary. Bash canonical identity excludes only description; all execution fields remain exact. Approval notices include the stored canonical call so compaction cannot lose retry arguments. Grant consumption requires exactly one affected row.

## Hook failure proof

Coordinator ran the harness against Claude Code 2.1.325 on 2026-11-14, using an authenticated personal account, with PROOF_FAILURES_ONLY=1 in default mode and again in auto mode. All eight crash/timeout cases passed: hook exit 1 and a 20s hang past timeout 2s, each in print and interactive PTY, produced exactly one PermissionRequest, zero executions, and a continued final response. With onFailure:block both failures block the call without human input. PTY dialogs flash then clear; no silent allow occurred. Interactive transcripts persist. PROOF_BASE must be inside an already trusted directory; the 2.1.325 trust screen does not accept this driver's scripted answer.

## Risks / Trade-offs

- Bash filesystem state and remote connector state can change after review → short expiry, exact arguments, human-visible input; Solo still decides whether retry remains appropriate.
- Hook failure/timeouts → return a model-visible denial; inline onFailure:block prevents execution when hook process fails.
- PermissionRequest has no tool_use_id → deduplicate by canonical identity and consume with a transactional compare-and-swap.
- No headroom in the Codex worker's default Claude account → coordinator performed the live proof; ordinary tests use fake hooks/sessions only.

## Migration Plan

Add the local SQLite table on open. Re-record changed supervisor spawn digests and bump surfaces. Install binary, restart daemon and supervisor, then restart Solo so its new hook/settings/token are loaded. Rollback requires restarting Solo with the prior binary; queued rows remain local data.

## Open Questions

None; implementation must retain the named exclusions and verified permission boundary.
