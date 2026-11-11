# Design

## D1. One predicate: "session not running"

A role/task is **disconnected** when it is bound to a task that is supposed to have a session, the daemon's session list was read successfully this tick, and the task is absent from the daemon's *alive* set.

- Source: `runner.RunningAndIdle()` already returns the daemon's alive set (`client.RunningAndIdle` filters `Alive`). It is the daemon/supervisor view, so it is independent of this TUI's own stream health. Mechanism B (alive, stream lost) is therefore NOT disconnected, and gets repaired instead (D4).
- **Fail open.** `client.RunningAndIdle` returns `(nil, nil)` on RPC error, indistinguishable from "no sessions". Add `SessionListHealthy() bool` to the daemon client (true iff the last `Daemon.ListSessions` succeeded; in-process runner is always healthy) read via an optional interface. When unhealthy the disconnected set is empty.
- **Grace.** A task must be absent for 2 consecutive ticks (~2s) before it shows `○`, so a daemon bounce or a session mid-restart (`pendingRestart` is already in the running list) never flashes it.
- Exposed as `heramodel.RoleView.Disconnected` (new field fed by `HeraPage.SetDisconnected(ids)`) and a `Disconnected` input on `TaskStatusInputs`. A pure `agent.DisconnectedIDs(tracked, running, healthy, prev) (ids, next)` function owns the grace logic so `bounce-recovery` can read the same set.
- Scope: only roles with `Live` bindings and tasks in `in_progress`/`in_review`. Pending/complete/archived are never disconnected (no session expected).

## D2. Rail precedence

`needs-input → active spinner → disconnected → accepted → ready_to_close → failed → done → idle → live`.

- needs-input and active outrank it: a role that is blocked on a prompt or producing output demonstrably has a session (and disconnected is false whenever the task is running, so this is belt-and-braces).
- Disconnected outranks accepted/ready_to_close/failed/done: those are stale-able resting states, and a dead agent behind a stale `ready_to_close` is exactly the case that looks alive today. Aaron asked for it to be visible.
- Exception: **accepted (task complete) never shows disconnected** (a finished agent is expected to be gone), enforced in the predicate scope above (complete excluded), not the icon switch.
- Glyph `○` (U+25CB) in `theme.StyleDimmed` (grey). Archived dim style still applies.

## D3. Dead-pane marker

`TerminalPane.Draw` already overlays `drawReasonBanner` on row 0 when a dead pane replays a log and `placeholderNote` is set. Add a default note, used when the pane has a `taskID`, the session is nil/dead, it is replaying content, and no `placeholderNote` is set: `stopped — press Enter to resume`. Closed-out banner, launch splash and the "Session not running" placeholder keep precedence (unchanged). No new slot or widget.

## D4. Don't mislabel live agents

- **Tasks-tab tick re-resolve.** Reuse the `reconcileOne` rule: if the agent pane holds a dead handle and `runner.Get(taskID)` returns a live, different handle, `ResetVT` + `SetSession`. Extracted as a shared helper used by both tabs so they can't drift.
- **Tasks-tab Enter.** Before `startSession`, ask `runner.HasSession`/re-resolve: if the daemon reports the session alive, attach the fresh handle (D4 first bullet) and return without touching task status. Only a genuinely absent session reaches `startSession`.
- **Stream reconnect.** Replace 3x500ms with bounded exponential backoff (500ms, 1s, 2s, 4s, 8s ≈ 15s total, capped). Each attempt re-checks `client.closed`/`rs.done` as today. uxlog `stream: reconnected task=… after N attempts` on success and `stream: gave up after … task=…` on exhaustion. Exhaustion still routes to `removeSessionStreamLost` (unchanged semantics).

## D5. Testing

Pure-function tests for the predicate (fail-open, grace, scope), `RoleStatusIcon`/`TaskStatusIcon` precedence tables, a SimulationScreen smoke test for the dead banner and rail glyph, a Tasks-tab tick test with a fake runner returning a fresh handle, an Enter test asserting no status flip and no `startSession`, and a stream test with a fake dialer that fails N times then succeeds.
