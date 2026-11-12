# Show disconnected agents and stop mislabeling live ones

## Why

Aaron returns to an agent after ~20 minutes, types, and nothing happens; pressing Enter "redraws and it's back". Two distinct causes, both traced in code:

- **Mechanism A (agent really died, the common case).** A session-supervisor restart (or any process death) kills the agent. The pane replays the dead session's last frame with no marker, so it looks alive. Keys are dropped (`hera/panes.go` forwardKey → `terminalpane.go` input handler, which only acts on Enter) and Enter goes through `heraReattach` → `startSession`. The "Session not running – press Enter" text only appears when there is no saved log.
- **Mechanism B (agent alive, TUI lost its stream).** The stream reconnect in `daemon/client/stream.go` is 3 retries x 500ms (~1.5s). A daemon restart outlasts that, so streams drop into `removeSessionStreamLost`. The Projects (Hera) tab re-resolves dead handles each tick (`reconcileOne`); the Tasks tab does not (its tick only rebinds when the pane has NO session), so the dead handle sticks until a TUI restart. Tasks-tab Enter then calls `startSession` on a live agent, fails with "session already exists", and flips the task to Pending.

Nothing in the rail tells the user which agents are dead.

## What changes

1. **Disconnected rail icon.** A role whose session is not running shows a dim hollow circle `○` in the Hera rail, plan-view nodes and the coordinator roster. (Not in the Tasks list; see Non-goals.) Backed by one reusable liveness predicate that fails open (unknown ≠ disconnected).
2. **Dead-pane marker.** A pane replaying a dead session's log shows a right-aligned "stopped" marker on its top border (the slot the re-render indicator uses; a row-0 overlay would hide replay content), so a dead pane never looks alive. Not shown for complete tasks.
3. **Don't mislabel live agents.**
   - The Tasks-tab tick swaps in a fresh handle when the daemon still reports the session alive (parity with the Hera tab).
   - Tasks-tab Enter checks liveness first and reattaches instead of calling `startSession`: no "already exists", no Pending flip.
   - Stream reconnect uses bounded backoff measured in seconds (survives a daemon restart), with uxlog on success and on give-up.

## Non-goals

- Automatic wake-up of agents after a supervisor restart (sibling role `bounce-recovery`). This change only exposes the liveness predicate it can read.
- **Frontend parity (named follow-ups):** the web SPA and macOS app do not get the `○` icon or dead banner in this PR. Both are REST clients; `/api/tasks` already carries per-task session liveness, so the follow-ups are `web-disconnected-icon` and `mac-disconnected-icon`. No REST/wire change is made here, so the parity trigger (REST surface change) is not hit.
- **Tasks-list `○` (removed in review):** the Tasks list gets no disconnected glyph. Reconciliation flips a dead in_progress plain task to in_review in the same tick, so it would almost never render, and `○` is already the Tasks-list pending glyph in a near-identical grey. The "stopped" pane marker is the Tasks-list signal.
- Changing session start/resume semantics or the Hera closed-out banner flow.
