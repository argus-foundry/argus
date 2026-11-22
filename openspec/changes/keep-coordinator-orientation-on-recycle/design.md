## Context

The coordinator orientation is delivered **once**, as part of the first prompt:

- `internal/tui/heraactions.go:220` (TUI New Coordinator) passes `HeraCoordinatorOrientation(base) + "---" + prompt` as the *task* prompt, but `RolePrompt: prompt` on its own, so the role row never stores the orientation.
- `internal/daemon/graduation.go:220` sends it as a task message when a resident worker graduates.
- `internal/heragater/heragater.go:615-617` builds `HeraSubCoordinatorOrientation + HeraCheckInOrientation + goal` for a `kind=subcoord` node, but `MaterializeHeraSubCoordinator` (`internal/agent/hera_spawn.go:644`) stores only the goal as the coordinator role's prompt.
- `hera_new_orchestrator` (`internal/mcp/hera.go:751-778`) never delivers any orientation, even though the base spec already says a coordinator created this way receives the discipline text (coordinator-context-management, "Every newly spawned coordinator receives the discipline text").

On recycle, `hera.BuildRecycleSeedPrompt` builds board summary + recycle framing + plan state + handoff note + last exchange + relay directive + original prompt (`role.Prompt`). Then `restartSessionWithSeedPrompt` sets `task.Prompt = seed`. The orientation is not in `role.Prompt` and is now gone from `task.Prompt`, so it is lost for good.

The resident already solves the same problem: `buildResidentRecycleSeedPrompt` re-appends its orientation at every recycle under "## Persistent orientation (background only)", and starts with `lastexchange.SystemTurnPrefix`.

Skills, routing text, the plugin dir, MCP config and `ARGUS_TASK_ID` are re-applied by `BuildCmd` at every launch (Codex: per-launch `CODEX_HOME` and generated AGENTS.md), so they are not affected.

## Goals / Non-Goals

**Goals:**

- Every coordinator recycle seed carries the right orientation, no matter how many recycles came before.
- Coordinators bound through `hera_new_orchestrator` get the orientation.
- A second recycle doesn't quote the first recycle's seed as the user's last message.
- Worker, freelance and resident recycle seeds don't gain any orientation.

**Non-Goals:**

- Rewriting the orientation text or the hera skill.
- Re-delivering orientation to a coordinator's *running* session (only spawn, the tool result and recycle).
- Changing the first-spawn prompts (TUI form, gater, graduation).

## Decisions

### D1. Work out the orientation at recycle time instead of storing it

`hera.CoordinatorOrientation(store, role, taskID)` works out which orientation applies from the role's live structure each time it's called:

1. If the role isn't a coordinator, return `""`.
2. Look up the coordinator's orchestrator. If that fails, log `[recycle]` and return `""`; the recycle still proceeds.
3. Scan the task's **live** bindings for a **worker**-kind role in a *different* orchestrator, taking the oldest first. This is the "bridge" shape: a sub-coordinator plan node, or a worker that promoted itself.
   - **Bridge found (sub-coordinator):** `HeraSubCoordinatorOrientation(orch.Name, parentOrch.Name, role.Name)`. If that parent worker role's `NodeKind` is `subcoord` (a gater-materialized node), append `HeraCheckInOrientation(parentOrch.Name, parentCoordName)`, where `parentCoordName` is the parent orchestrator's first coordinator role (default `"coord"`, the same rule the gater uses).
   - **No bridge (top-level):** `HeraCoordinatorOrientation(orch.Name)`.
   - **Error listing bindings:** log it and fall back to the top-level orientation, since more rules is safer than none.

The store interface needs only `HeraOrchestrator`, `HeraRole`, `ListHeraRolesByKind` and `ListHeraLiveBindingsByTask`. Both `*db.DB` and the MCP `HeraStore` already have all four, so the MCP tool and the recycle seed share one function.

**Why not store it?** Saving the orientation text on the role or in `task_meta` at spawn time would freeze the orchestrator name and the parent link. Both can change: orchestrators get renamed, and graduation turns a resident-team worker into a top-level coordinator (graduation ends the worker binding with reason `graduated`, so the live-binding scan correctly sees it as top-level). Storing it would also need a column or meta key and a backfill for coordinators that already exist. Working it out each time fixes every existing coordinator on its next recycle, with no migration.

Sub-coordinator plan nodes use the real child orchestrator name here. At materialize time the gater has to use the node name as a placeholder, because the de-duplicated child name doesn't exist yet. At recycle time it does.

### D2. Where it goes and how it's framed

The section sits after the relay directive and **before** the `---` / "Original prompt (background only …)" section, so the "background only" label on the original prompt can't seem to cover the rules:

```
## Standing coordinator orientation (still in force)
These are the standing rules you were launched with. They still apply, but they are not a new task: act on the state, handoff note, and open items above.
[sub-coordinator plan node only:] Your prior session already completed the launch check-in below; do not check in again or wait for a new 'go' unless your handoff note says you were still waiting for one.

<orientation text>
```

Unlike the resident's "background only" heading, these are *rules that still apply*, not old instructions. So the heading says "still in force", and the next sentence keeps the session from treating the text as a fresh task.

**Decision for Aaron — re-sending the sub-coordinator's check-in text.** The check-in text tells a session to message its parent and wait for "go" before doing any work. Options:

- **(a) Include it, with the "already checked in" line (recommended).** This keeps the check-in text exactly as acceptance asks, but stops a recycled sub-coordinator from going back into a wait for a "go" that never arrives.
- **(b) Include it word for word.** Simplest, but a recycled sub-coordinator may re-ping its parent and block on `hera_inbox` until someone answers.
- **(c) Leave it out of recycles.** Shortest seed, but a sub-coordinator loses the "your parent decides go/wait" relationship text entirely.

### D3. `hera_new_orchestrator` delivers the orientation in its result

After a successful bind, the tool result adds a `## Coordinator orientation` section after the existing key/value block, with `CoordinatorOrientation(...)` for the new role. A plain task that promotes itself gets the full top-level text; a worker that promotes itself gets the sub-coordinator text (its parent role is a plain worker, so no check-in). Its recycles then carry the same text through D1.

**Decision for Aaron — how a `hera_new_orchestrator` coordinator gets its rules.** Options:

- **(a) In the tool result now, and in every recycle (recommended).** The rules arrive the moment the session becomes a coordinator, and they come back after every recycle. Cost: about 2.5k tokens, once, in that session.
- **(b) Recycle only.** Saves those tokens, but the session runs without the rules until its first recycle, which for a short-lived one may never happen.
- **(c) A task message (doorbell) after binding.** It arrives as its own turn, but it's delivered later than the tool result and needs the message bus. That adds a moving part for no gain over (a).

### D4. `[argus] ` marker on recycle seeds

The double-recycle bug is real. `lastexchange.Extract` treats any non-meta user turn as "human" unless `systemTurn` sees the `[argus] ` prefix. The fresh session's first user turn *is* the seed. If no human message follows before the next recycle, the next seed quotes the previous seed as "the user's last message". `BuildRecycleSeedPrompt` and `buildTaskRecycleSeedPrompt` now start with `lastexchange.SystemTurnPrefix`. The resident seed already does. This applies to worker and freelance hera seeds too, because the bug affects every role. That is the only change to those seeds.

### D5. `hera_new_orchestrator` keeps the original prompt

When `prompt` is empty, the coordinator role's `Prompt` falls back to the calling task's `Prompt`. That way the recycle seed's "Original prompt" section shows what the session was doing before it promoted itself, instead of nothing. It costs nothing extra and needs no schema change. If the task was itself recycled before promoting, its prompt is that earlier seed, which is still accurate background.

## Risks / Trade-offs

- [Seed grows by about 2.5k tokens per coordinator recycle, and the board rules partly overlap `BoardSummary`'s fixed text] → It's a one-off cost per fresh session, and much smaller than a coordinator running without its rules. Removing the overlap is out of scope, because it would mean rewriting the orientation.
- [A coordinator that also holds a worker binding in an unrelated orchestrator is treated as a sub-coordinator] → This is the same shape as a worker that promoted itself, and the two can't be told apart from the data. The sub-coordinator text is still correct for it, since it does answer to that orchestrator.
- [The structure lookups fail at recycle time] → Fail-open (D1 steps 2–3). The recycle is never blocked, and the failure is logged.
- [The `[argus] ` prefix shows in the session's first message] → The resident seed already shows it.

## Migration Plan

None. Coordinators that already exist pick up the orientation on their next recycle. Rollback means reverting the commit.

## Open Questions

- The two decisions above (D2 check-in handling, D3 delivery route). Defaults are the recommended options.
