## Context

`add-resident-agent` (PR #1191) made the resident a singleton `kind=resident` task in `~/.argus/resident/` (not a git worktree), supervisor-owned, excluded from every task enumeration via `IsResident()`, launched with a self-contained permission policy (`--setting-sources project`, inline allow/ask/deny, bypass modes disabled, `ARGUS_RESIDENT=1`, MCP connection bound with `?caller=<taskID>`), recycling itself through the role-free `task_recycle` primitive with a vault-re-read seed.

Hera already provides what delegation needs: roles bound to tasks, `hera_spawn_worker` (born-bound worker via `agent.SpawnHeraWorker`), the idle-gated `notify.Notifier` bus, `hera_inbox`, `hera.NukeSingleRole`, `internal/mergesafety`, the needs-input detector, and the Hera view. The resident has no Hera role today and is filtered out of the roster.

Constraints from AGENTS.md: breaking changes are fine (one user); never branch on backend identity outside `internal/agent/backends.go`; every behavioral change evaluates TUI/web/macOS parity; the resident pane must not use the Hera size-drift kick machinery.

## Goals / Non-Goals

**Goals**

- The resident delegates to a standing team over the existing Hera primitives, with no second delegation mechanism.
- `hera_delete_worker` exists, is resident-only by a code guard, and is safe by default (merge-safety first).
- A resident-spawned worker cannot wedge on `AskUserQuestion` or a permission prompt, on any backend family it can route to.
- The resident tab shows the team and its stuck agents without offering creation.
- Resident recycle and coordinator recycle are one mechanism; live workers survive resident recycle/restart.

**Non-Goals**

- **Web SPA and macOS app surfaces.** The resident has no web/macOS surface today (TUI only, per `add-resident-agent`). Named follow-ups: a web resident view and a macOS resident view that render the team rail, pane and inbox; until then the resident's workers appear on those clients only as ordinary tasks. The standing orchestrator is excluded from `/api/hera` (D7), so web/macOS Hera tabs do not show it. The macOS Hera tab stays read-only per AGENTS.md.
- Sub-coordinators, plan-DAG nodes (`hera_plan*`, `hera_block`) and multiple resident orchestrators. v1 is one flat team.
- Human mutation actions (nuke, hide, pin, rename, status, kanban) from the resident rail (see Open Questions).
- Changing the resident's outward-connector tiers.
- Working in the resident's own directory: workers always get their own worktree in a named project.

## Decisions

### D1. One standing orchestrator, daemon-created, reserved name

The resident manager, on its first successful start (after the row exists, before the session launches), ensures orchestrator `resident` with one coordinator role (name `resident`) bound to the resident task, in a single transaction via `CreateHeraOrchestrator` (already idempotent) plus `CreateHeraRoleWithBinding`. The binding's worktree path is the resident working directory. Every later start finds the existing live binding and does nothing. The role name is fixed (not `resident.name`) so renaming the agent never rewrites Hera rows or worker prompts; display uses the name.

`resident` is a reserved orchestrator name: creating a different orchestrator with it (TUI form, `hera_new_orchestrator`, REST) is rejected, and if a user orchestrator already holds it the daemon refuses to start the resident coordinator and surfaces the reason through the resident state `Reason`. The role kind stays `coordinator`; "is the resident" is `task.IsResident()`, so no schema CHECK change.

**Alternative: let the resident call `hera_new_orchestrator` itself.** Rejected: it would be unreliable (depends on the model remembering), would let it name or create several orchestrators, and runs into the existing guard (D6).
**Alternative: one orchestrator per errand.** Rejected: the user's requirement is one standing team, and per-errand orchestrators multiply the rail and the recycle surface.

### D2. Spawning: project is mandatory, spawn is ask-tier

`hera_spawn_worker` for a resident caller SHALL require `project` and SHALL reject an unregistered one, because the resident has no repo to default from. The call stays on the ask list so each spawn is a one-key confirmation (cost and autonomy are real). Everything else about spawn is unchanged: `agent.SpawnHeraWorker`, branch namespacing `argus/resident/<role>`, `[backend_routing]`.

### D3. `hera_delete_worker`: exclusive by connection-bound identity

The tool takes `role` (name or id within the resident orchestrator) and optional `force` (default false). The guard has three independent layers, all in the MCP handler:

1. **Caller identity is the `?caller=<taskID>` bound at spawn**, the same mechanism `task_recycle` uses, NOT the self-reported `cwd`/`id` the other hera tools accept. The handler ignores `cwd`/`id` entirely. A call on a connection with no bound caller, or whose caller is not `IsResident()`, fails with an explicit "resident only" error before any lookup. Tool listing hides the tool from non-resident connections (defense in depth; the handler is the enforcement).
2. **Scope:** the target role must belong to the resident's standing orchestrator and be `kind=worker`; the resident's own role (or any role whose task `IsResident()`) is rejected as "never itself".
3. **Not exposed elsewhere:** no REST endpoint, no TUI key. The human path (`POST /api/hera/roles/{id}/nuke`) is unchanged and is not a bypass of the safety default because it is the human's own action.

Execution reuses `hera.NukeSingleRole` (stop session, archive-and-reclaim worktree + branch + task). Because `NukeSingleRole` returns a background closure and does not report refusal, the tool wraps it:

- **Default (force=false):** classify the worker's task with merge-safety first. A shared helper `mergesafety.ClassifyTask(ctx, task)` (extracted from `internal/api/cleanup_candidates.go`'s candidate construction so there is one construction path) returns a Verdict. Proceed only if `Safe`, OR the work is **empty** = no commits ahead of the worker's base ref and a clean `git status --porcelain` in its worktree (empty is checked locally, fails closed). Otherwise refuse with a reason naming the verdict (for example "not safe: no merged PR for branch argus/resident/foo; commits ahead: 3") and change nothing.
- **force=true:** skip classification and nuke. The result states what was destroyed (branch, unmerged commit count if cheaply known) so the resident's log and its message to Aaron stay honest.
- The tool is on the ask list, so Aaron sees the confirmation and its arguments including `force`.

Merge-safety fails closed everywhere (network failure means not safe), so a transient GitHub outage refuses a default delete; the resident can retry or use `force`.

**Alternative: reuse the REST nuke from the resident.** Rejected: no refusal semantics, and it would put a destructive human endpoint on an agent.
**Alternative: allow any coordinator and gate by config.** Rejected by requirement; exclusivity needs a code guard.

### D4. Workers must never block: three layers, per family

Stuck points for an unattended worker: (i) a permission prompt for a tool/command, (ii) `AskUserQuestion`, (iii) plan-mode entry/exit approval. All three are interactive prompts nobody answers.

**(a) Bypass only when the Argus sandbox wraps the worker.** A resident-spawned worker carries a marker on its task (`task_meta(hera, resident_worker)`, stamped in the same `AfterPersist` hook that stamps the role, so it exists before launch and survives resume). `BuildCmd` for a marked task: if `argusWrapsSandbox(task, cfg)` is false the launch is refused with a "resident worker requires the Argus sandbox" error (mirroring the resident's own "sandbox profile failure fails closed" rule), and `hera_spawn_worker` returns that error to the resident instead of creating a half-started task (a pre-flight check in the spawn path, plus the BuildCmd check as the backstop for resume/restart). If it wraps, the family's bypass-active permission flags are applied regardless of `defaults.permission_mode`. The sandbox is what makes bypass tolerable: file writes confined to the worktree grants, network per the sandbox profile.

Known limit: a user-level managed `disableBypassPermissionsMode` in Claude settings would make the bypass flag inert. The worker does not use `--setting-sources project` (it must keep the user's plugins/skills/hooks like any worker), so this is a user-config risk; layer (c) is the backstop and the spawn logs the effective mode.

**(b) Remove the question tools; redirect through Hera.** A new per-family capability `Family.UnattendedWorker` (in `internal/agent/backends.go`, per the registry rule) declares how that family closes stuck points. Claude: `--disallowedTools AskUserQuestion EnterPlanMode ExitPlanMode` appended to the launch. The worker orientation (added to `HeraWorkerOrientation` for marked workers only) says: you cannot ask the user; if you need a decision, call `hera_send(status=blocked)` to the resident with the question and the options you see, then stop and wait. The resident either answers, relays the question to Aaron in its own pane (it is the user-facing agent), nudges, or force-deletes.

**(c) Backstop.** The existing daemon needs-input detector already flags any live role. A new notification: when a worker in the resident orchestrator is flagged needs-input (detector or self-reported `blocked`), the daemon sends the resident a Hera message through the normal bus: "worker X appears to be waiting on a prompt: <tail excerpt>, options: reply with hera_send, nudge, or hera_delete_worker". It is edge-triggered per flag episode (one message per raise, none while the flag stays up), via the same exactly-once notifier. The detector tick runs in the REST server today (`internal/api/push.go`, which also owns `residentWaiting`); the hook point is where that tick already computes the sticky set.

**Backend families.** The resident's `[backend_routing]` can send workers to Codex and others. The eligibility rule: a resident worker may run only on a family whose `UnattendedWorker` capability is non-nil. A routing pass filters tiers to eligible backends (the same pattern `internal/agent/hera_spawn.go` already uses to filter coordinator backends to Claude), and an explicit `backend` naming an ineligible family is rejected with an actionable error.

| Family | Permission prompts | Question/plan tools | Verdict |
|---|---|---|---|
| claude | bypass-active flag, Argus-sandbox-gated | `--disallowedTools` | eligible |
| codex | default command is already `codex --dangerously-bypass-approvals-and-sandbox`: approvals never prompt, and Codex's own sandbox is OFF so the Argus sandbox is the only wall, making the gate in (a) essential | Codex's structured user-input tool is not yet confirmed unreachable | **eligible only if the verification task confirms it**; otherwise filtered from resident-worker routing |
| pi, opencode, agy, cursor, omp, custom | opencode `--auto` approves asks; agy/cursor/omp have mode flags; pi/custom none | no verified disable mechanism | **ineligible in v1** |

Rationale: "equivalent mechanism or pin to families where the stuck points can be closed" is satisfied by Claude now, Codex after an explicit spike (tasks 4.x), and everything else pinned out. Pinning out is the safe default because (c) alone is a backstop, not a guarantee. Adding a family later means filling its `UnattendedWorker` and one backends.md row.

**Alternative: only the backstop (c).** Rejected: it recovers from, rather than prevents, wedges, and the resident is often idle or asleep from Aaron's view.
**Alternative: Claude-only workers.** Simpler, but throws away Codex budget routing, a stated user priority; the eligibility filter keeps that door open without guessing.

### D5. One recycle mechanism

Today there are two: the resident's `task_recycle` (agent-initiated at a quiet point, seed = orientation + handoff + "re-read open-loops") and the coordinator path (`hera_status` `request_recycle` flag, `RecycleWatcher`, human-forced `recycle_coord`, seed = plan state + handoff note + mission). Giving the resident a coordinator role would make both reachable and racing (the task-recycle runner already has an in-flight guard; the Hera one has its own).

Decision: **the runner is `taskRecycleRunner`; Hera triggers are only triggers.** For a task where `IsResident()`, `HeraRecycleRunner.Restart` (called by `recycleCoord` from the watcher, the forced RPC and the TUI `B` key) delegates to the same `restart` path `task_recycle` uses (stray-job cleanup, `restartSessionWithSeedPrompt`, resident PTY role, resident seed), and the `inFlight` guard is shared so a `task_recycle` and a Hera-triggered recycle for the resident cannot overlap. The idle-wait is `taskRecycleRunner`'s. The seed stays resident-shaped (orientation, handoff, open-loops-first) and gains a **team section** built from the standing orchestrator's roster (role name, project, status, last self-reported status line, unread-from-worker count, any worker flagged blocked), via the existing `buildPlanStateSection`-style builder, so the fresh session knows its team without a tool call. Budget: `resident.context_budget` (the coordinator budget is not applied to the resident).

**Alternative: let the coordinator path win and retire `task_recycle` for the resident.** Rejected: the coordinator seed does not re-read the vault, and the resident's memory contract (open-loops first) is the thing that makes recycle safe for it.

### D6. Interaction with the `hera_new_orchestrator` guard

The existing guard rejects a caller that already coordinates a DIFFERENT orchestrator, and for the same orchestrator falls through to "already the coordinator". With a standing orchestrator the resident is always in the first case: any name other than `resident` is rejected by the existing guard, and `resident` itself reaches the same-orchestrator rejection. That is correct behavior, but two properties are not guaranteed by it: the existing guard scan is fail-open on a store error, and its message tells a coordinator to author a `kind=subcoord` plan node, which the resident does not have.

Decision: add an explicit resident check at the top of the handler (before the guard scan), fail-CLOSED: a caller with `IsResident()` is rejected with "the resident's team is its standing orchestrator; spawn workers with hera_spawn_worker". The tool is also on the resident's deny list. The daemon's own creation path (D1) never goes through this handler, so the guard cannot block it. The reserved-name rejection (D1) is checked in the same handler for all other callers.

### D7. The standing orchestrator is hidden from the generic Hera surfaces

The resident is excluded from the Hera roster today by `IsResident()`; its orchestrator is hidden the same way: excluded from the Projects tab model, the jump switcher's Hera column, `/api/hera*` roster and cleanup roll-ups, and from the cascade-nuke/cleanup candidate sets (so no generic sweep reclaims the team behind the resident's back). The standing orchestrator and the resident coordinator role cannot be nuked, hidden or detached through any generic path (the existing resident lifecycle-verb rejection extends to the role/binding verbs). Workers are ordinary tasks: they appear in the Tasks list (and fall under the hera-managed hide toggle) and open normally. `TestResidentExcludedEverywhere` gains the orchestrator/role choke points.

### D8. View: the Hera view with exactly one coordinator, in resident mode

The resident tab reuses `HeraPage` in a **resident mode** rather than forking a new view: the model is the single standing orchestrator; the rail lists its workers; selecting the coordinator row shows the resident pane (live, input to PTY, same resident pane lifecycle as today: first-attach forced resync, no size-drift kick, reason banner); selecting a worker row shows that worker in the existing worker pane/details; `i` opens the existing read-only inbox viewer for the resident's inbox, and the rail header carries the unread count and a `?`/blocked marker per worker so stuck agents are visible at a glance (the existing needs-input glyph and ready-to-close icons, unchanged).

**Navigation-only** is enforced structurally: resident mode sets a rail-key allowlist (move, fold, Enter, `i`, fullscreen toggle, copy, focus) in `handleRailMutation`'s gate (`isRailMutationKey` already classifies mutation keys); every spawn / new coordinator / move / join / detach / hide / pin / rename / status / kanban / nuke key is inert in resident mode and absent from the `?` overlay and ctrl+k palette for that context. A smoke test asserts each is inert. Keymap changes follow `gotchas/keybindings.md`: the allowlist is data in `keymap`, the help rows are generated.

The existing resident requirements stay: `global.resident` toggle and exact return, rail box, tab-label marker, no size-drift kick on the resident pane.

### D9. Permissions

`[resident]` defaults change as follows; `resident_inventory_test.go` classifies every hera tool so a new one cannot land in allow by accident.

- **Allow:** `hera_send`, `hera_inbox`, `hera_get_messages`, `hera_mark_read`, `hera_status`, `hera_tree_updates`, `hera_accept`, `hera_revive`, `hera_unblock`, `hera_board_list/post/update/resolve`. Each acts only within the caller's own orchestrator because the resident's connection is bound; none creates sessions or destroys work.
- **Ask:** `hera_spawn_worker`, `hera_delete_worker`.
- **Deny:** `hera_new_orchestrator`, `hera_join`, `hera_move`, `hera_rebind`, `hera_plan`, `hera_plan_node`, `hera_plan_node_update`, `hera_plan_node_cancel`, `hera_block` (no plan DAG or membership surgery in v1).
- Outward-connector tiers and the existing allow entries are unchanged. `hera_accept` completes a worker's task, which is a lifecycle write, but it is the coordinator's normal accept step and non-destructive; it is allowed deliberately (open question below if Aaron wants it on ask).

Note `task_inbox` / `task_message_ack` stay allowed (resident-to-resident bus for non-Hera senders); Hera workers address the resident through `hera_send` and the resident reads `hera_inbox`. The orientation prompt is updated to say which inbox to read for what.

### D10. What happens to workers when the resident recycles or restarts

Workers are separate supervisor-owned sessions bound to the standing orchestrator; nothing about the resident's session lifecycle stops them.

- **Recycle:** only the resident session is killed and relaunched. The role binding stays live (the row and binding are not ended). Worker messages sent meanwhile are durable in the Hera inbox; doorbells are idle-gated and deliver when the fresh session is idle. The seed's team section (D5) re-orients it.
- **Crash/daemon bounce:** same; the daemon re-attaches the supervisor-owned sessions. The existing startup hera-binding reconciliation and finished-session reaper already spare live coordinator bindings; the resident binding counts as one.
- **`resident.enabled` becomes false:** the resident session stops; its row, binding, orchestrator and the vault are intact; **workers keep running** (stopping them mid-task would lose uncommitted work). Settings shows "N workers still running"; workers that report blocked queue their message. Re-enabling resumes the same binding. This is deliberately lenient; the alternative (stop workers too) is an open question.
- **Prerequisite failure stop** (two consecutive ticks) behaves like disable for workers.

## Alternatives considered

- **A separate "resident team" mechanism** (not Hera): rejected, it would duplicate spawn, bus, merge-safety and the rail.
- **Making the standing orchestrator visible in the Projects tab:** rejected; the Projects tab has creation actions and a second entry point to the same team defeats the navigation-only requirement.
- **Bypass mode for all hera workers:** rejected; only resident-spawned workers are unattended by design, and bypass without the Argus sandbox is unacceptable.

## Risks / Trade-offs

- Bypass mode plus the sandbox concentrates trust in the sandbox profile; a profile gap becomes unattended arbitrary command execution in a worktree. Mitigation: refuse-if-unwrapped, ask-tier spawn, the existing sandbox gotchas.
- `--disallowedTools` removes the tools but a model may still try to ask in free text and idle. The needs-input backstop (c) and the orientation cover this.
- Merge-safety refusing on a transient network failure can make cleanup feel sticky; `force` is the escape and is on the ask tier.
- Delete is destructive and agent-initiated; the ask tier plus default safety plus the explicit `force` flag are the three brakes.
- Codex eligibility is unverified; defaulting to filtered-out until the spike confirms avoids shipping a hole.
- Two recycle triggers now converge on one runner: the shared in-flight guard must be exercised by a test where both fire.

## Migration Plan

No data migration. On first start after upgrade the daemon creates the orchestrator, role and binding for an existing resident row. Existing resident sessions pick up the new allow/ask/deny policy and orientation on their next launch (recycle or restart); until then they have no hera tools in their policy, which is harmless.

## Acceptance criteria

- Standing orchestrator exists exactly once, survives resident restarts, and is invisible to Projects/REST roster/cleanup.
- `hera_delete_worker` from any non-resident connection (including a regular coordinator and a resident worker) is rejected; from the resident it deletes only its own workers, refuses itself, and refuses unmerged non-empty work unless `force`.
- A resident worker cannot launch without the Argus sandbox; on Claude it cannot call `AskUserQuestion`/plan tools; an ineligible backend is never picked by routing.
- A needs-input flag on a resident worker produces exactly one message to the resident per episode.
- Resident tab rail has no creation key live.
- A recycle triggered by `task_recycle` and by `recycle_coord` on the resident run the same code path and never overlap; workers are untouched.

## Open Questions

1. Should the human be able to nuke/hide/rename workers from the resident rail (still no creation), or strictly navigation only? v1: strictly navigation only.
2. When the resident is disabled, should its workers be stopped or left running? v1: left running.
3. Is `hera_accept` (marks a worker's task complete) fine on the allow list, or should it be on ask?
4. Codex workers: should Codex be eligible for resident workers at all if the spike confirms the stuck points are closed, or stay Claude-only for simplicity?
5. Should every worker spawn ask for confirmation, or only spawns on projects the resident has not used before?
