## Why

A coordinator forgets how to be a coordinator the first time it is recycled. A brand-new coordinator starts with the long coordinator rulebook: dispatch, don't implement; how to use the board; when to archive; never call `hera_new_orchestrator` on yourself; open PRs with iris; and the five habits for a long-lived coordinator. The recycle seed rebuilds the session's prompt from the board, the plan state, the handoff note and the role's stored mission. It leaves that rulebook out, and it overwrites the task prompt, so the rules are gone for good from the first recycle on. Long-running coordinators are the ones that recycle, so they are the ones that end up running without their rules.

## What Changes

- **Recycled coordinators keep their rules.** Every recycle seed for a coordinator includes its standing orientation in a "Standing coordinator orientation" section, rebuilt fresh at each recycle from the role's current place in the tree. It never depends on the task prompt, so it survives any number of recycles.
  - A top-level coordinator gets the full coordinator orientation (`HeraCoordinatorOrientation`).
  - A sub-coordinator (a coordinator whose session is also a worker in a parent orchestrator) gets the sub-coordinator orientation (`HeraSubCoordinatorOrientation`). A sub-coordinator created from a `kind=subcoord` plan node also gets its check-in text, plus one line saying the launch check-in already happened.
  - Worker, freelance and resident recycles are unchanged.
- **Coordinators created with `hera_new_orchestrator` get the rules too.** Today they never see the orientation. The tool's success result now carries the same orientation text, and their recycles carry it as above.
- **A recycled session no longer quotes its own seed back as "what the user said".** Hera recycle seeds and plain `task_recycle` seeds now start with the `[argus] ` marker that the last-exchange reader already skips. Without it, a session recycled twice with no human message in between had the previous seed quoted to it as the user's last message.
- **`hera_new_orchestrator` keeps the session's original prompt when no `prompt` is given.** The coordinator role's stored mission falls back to the calling task's current prompt, so the "Original prompt" section of a later recycle seed isn't blank.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `coordinator-context-management`: a coordinator's recycle seed carries its standing orientation; hera recycle seeds are marked as Argus-injected.
- `hera-coordination`: `hera_new_orchestrator` returns the coordinator orientation and keeps the caller's task prompt as the role mission when `prompt` is omitted.
- `task-recycle`: the plain-task recycle seed is marked as Argus-injected.

## Impact

- `internal/hera/recycle.go`: `BuildRecycleSeedPrompt` gains the orientation section and the `[argus] ` prefix. A new `CoordinatorOrientation` helper works out which orientation applies from the role's live bindings.
- `internal/mcp/hera.go`: the `hera_new_orchestrator` result gains the orientation, and the role prompt falls back to the task prompt.
- `internal/daemon/task_recycle.go`: `buildTaskRecycleSeedPrompt` gains the `[argus] ` prefix.
- No schema, REST, wire-protocol or UI changes. The seed is plain text, so Claude and Codex coordinators get the same thing. The last-exchange reader only runs for Claude, so the marker matters only there.
