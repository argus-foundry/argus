## Why

The resident (the always-on assistant from `add-resident-agent`) can answer and draft, but it cannot delegate: it has no repo, no worktree and no way to hand an errand that needs code, research or a long-running session to anything but Aaron. Hera already solves delegation (coordinators dispatch workers over an idle-gated message bus), so the resident should be a Hera coordinator with its own standing team rather than a second, parallel delegation mechanism. Delegation is only safe unattended if workers can never wedge on a prompt nobody is watching, and only tidy if the resident can retire workers it no longer needs.

## What Changes

- The resident becomes a **Hera coordinator** of ONE standing orchestrator (reserved name `resident`), created idempotently by the daemon when the resident is first started. It is not created by `hera_new_orchestrator`. Workers are spawned with `hera_spawn_worker`, always naming `project` explicitly (the resident has no repo), and talk to the resident over the normal Hera bus and inbox.
- New MCP tool **`hera_delete_worker`**, exclusive to the resident, enforced in code (connection-bound caller identity, not documentation). It deletes only workers of the resident's own orchestrator, never the resident itself, by reusing `hera.NukeSingleRole`. By default it runs the merge-safety check first and refuses with a reason unless the work is confirmed merged or empty; `force=true` skips the check.
- **Workers never block on a prompt.** Resident-spawned workers (a) launch in bypass permission mode only when the Argus sandbox actually wraps them, and the spawn is refused otherwise; (b) have `AskUserQuestion`, `EnterPlanMode` and `ExitPlanMode` disallowed where the family supports it, with an orientation to ask the resident via `hera_send(status=blocked)`; (c) are backstopped by a needs-input notification to the resident. Resident workers route only to backend families that can close these stuck points; other families are filtered out of routing and rejected when named explicitly.
- The resident tab becomes a **one-coordinator Hera view**: rail of the resident's workers, the resident's conversation pane, and the inbox. The human can manage workers from the rail (nuke, hide, rename, role status) but never create them, and the resident is told when the human nukes, hides or renames one.
- **Worker backends are a setting**: `[resident] worker_backends` plus a Settings row listing only families with an implemented unattended mechanism. This change implements Claude and Codex (Codex gated on an early spike); routing among the enabled families still uses `[backend_routing]`.
- **Worker graduation**: a worker that finds its job is big asks the resident, the resident asks Aaron, and the new resident-only, ask-tier `hera_graduate_worker` turns the worker into a stand-alone top-level coordinator in the normal Hera rail, in one transaction, restarting it in place with resume and normal coordinator flags. Resident workers cannot self-promote with `hera_new_orchestrator`.
- **Permissions:** the coordination hera tools join the resident's allow list; `hera_spawn_worker` and `hera_delete_worker` are allowed without a prompt (a force delete's result carries an unmerged summary the resident must report in its conversation), and only the new `hera_graduate_worker` is on its ask list; `hera_new_orchestrator` and the plan/membership tools are denied. Outward-connector tiers are unchanged.
- **One recycle mechanism.** The resident keeps `task_recycle` (vault re-read seed); the Hera coordinator recycle triggers route into that same runner for the resident, and the seed gains a team section. Recycling or restarting the resident never touches live workers.
- The Stop-hook requirement that gates the resident lookup on "no Hera role" is **MODIFIED** (the resident now has one).

## Capabilities

### New Capabilities

- `resident-coordinator`: the standing orchestrator, worker spawning, exclusive delete-worker and graduate-worker, human worker management and resident notification, unattended-worker guarantees and the worker-backend setting, coordinator permissions, one recycle mechanism, worker survival across resident lifecycle events.

### Modified Capabilities

- `resident-agent`: the Stop-hook gate no longer assumes the resident has no Hera role.
- `resident-tui`: the resident tab is the one-coordinator Hera view (rail, pane, inbox) instead of one full-width pane.
- `hera-coordination`: reserved `resident` orchestrator name, `hera_new_orchestrator` guard interplay and resident-worker rejection, exclusive `hera_delete_worker` and `hera_graduate_worker`, the `graduated` end reason.
- `task-recycle`: resident seed gains a team section; Hera coordinator recycle triggers route to the one runner.
- `coordinator-context-management`: the resident's budget and nudge use `resident.context_budget` and the single recycle path.

## Impact

- **Code:** `internal/daemon` (standing-orchestrator ensure, recycle routing, needs-input notify), `internal/db` (reserved-name guard, resident-orchestrator filtering, `GraduateHeraWorker` transaction and `graduated` end reason), `internal/mcp` (`hera_delete_worker`, new_orchestrator guard, spawn validation), `internal/hera` (delete-worker primitive over `NukeSingleRole`, team seed section), `internal/agent` (per-family unattended-worker mechanism, backend eligibility filter, bypass-only-when-wrapped), `internal/mergesafety` + `internal/api/cleanup_candidates.go` (shared single-task classify helper), `internal/config` (`[resident]` allow/ask/deny defaults, `worker_backends`), Settings Resident row, `internal/tui` + `internal/tui/hera` (resident mode of the Hera view), `cmd/argus` coord-hook.
- **Docs:** README MCP-tool and `[resident]` Reference tables, `gotchas/resident.md`, `gotchas/hera-view.md`, `backends.md` (new "unattended worker" row).
- **Parity:** TUI only; web and macOS are named follow-ups (see Non-Goals in design.md).
