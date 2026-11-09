## Context

`add-resident-agent` (PR #1191) made the resident a singleton `kind=resident` task in `~/.argus/resident/` (not a git worktree), supervisor-owned, excluded from every task enumeration via `IsResident()`, launched with a self-contained permission policy (`--setting-sources project`, inline allow/ask/deny, bypass modes disabled, `ARGUS_RESIDENT=1`, MCP connection bound with `?caller=<taskID>`), recycling itself through the role-free `task_recycle` primitive with a vault-re-read seed.

Hera already provides what delegation needs: roles bound to tasks, `hera_spawn_worker` (born-bound worker via `agent.SpawnHeraWorker`), the idle-gated `notify.Notifier` bus, `hera_inbox`, `hera.NukeSingleRole`, `internal/mergesafety`, the needs-input detector, and the Hera view. The resident has no Hera role today and is filtered out of the roster.

Constraints from AGENTS.md: breaking changes are fine (one user); never branch on backend identity outside `internal/agent/backends.go`; every behavioral change evaluates TUI/web/macOS parity; the resident pane must not use the Hera size-drift kick machinery.

## Goals / Non-Goals

**Goals**

- The resident delegates to a standing team over the existing Hera primitives, with no second delegation mechanism.
- `hera_delete_worker` exists, is resident-only by a code guard, and is safe by default (merge-safety first).
- A resident-spawned worker cannot wedge on `AskUserQuestion` or a permission prompt, on any backend family it can route to.
- The resident tab shows the team and its stuck agents and lets the human manage workers (nuke, hide, rename, role status) without offering any creation; the resident is told when the human removes or hides one of its workers.
- Which backend families resident workers may use is a setting (`[resident] worker_backends`), listing only families with an implemented unattended mechanism; this change implements Claude and Codex.
- Big work graduates: a resident worker can become a stand-alone top-level coordinator in the normal Hera workstream, with Aaron's approval enforced mechanically.
- Resident recycle and coordinator recycle are one mechanism; live workers survive resident recycle/restart.

**Non-Goals**

- **Web SPA and macOS app surfaces.** The resident has no web/macOS surface today (TUI only, per `add-resident-agent`). Named follow-ups: a web resident view and a macOS resident view that render the team rail, pane and inbox; until then the resident's workers appear on those clients only as ordinary tasks. The standing orchestrator is excluded from `/api/hera` (D7), so web/macOS Hera tabs do not show it. The macOS Hera tab stays read-only per AGENTS.md.
- Sub-coordinators, plan-DAG nodes (`hera_plan*`, `hera_block`) and multiple resident orchestrators. v1 is one flat team.
- Human **creation** actions (spawn, new coordinator, move, join, detach) and pin/kanban from the resident rail. Nuke, hide, rename and role status ARE allowed (D8).
- Nesting sub-coordinators under the resident. Graduation (D11) makes a worker a top-level coordinator instead.
- Changing the resident's outward-connector tiers.
- Working in the resident's own directory: workers always get their own worktree in a named project.

## Decisions

### D1. One standing orchestrator, daemon-created, reserved name

The resident manager, on its first successful start (after the row exists, before the session launches), ensures orchestrator `resident` with one coordinator role (name `resident`) bound to the resident task, in a single transaction via `CreateHeraOrchestrator` (already idempotent) plus `CreateHeraRoleWithBinding`. The binding's worktree path is the resident working directory. Every later start finds the existing live binding and does nothing. The role name is fixed (not `resident.name`) so renaming the agent never rewrites Hera rows or worker prompts; display uses the name.

`resident` is a reserved orchestrator name: creating a different orchestrator with it (TUI form, `hera_new_orchestrator`, REST) is rejected, and if a user orchestrator already holds it the daemon refuses to start the resident coordinator and surfaces the reason through the resident state `Reason`. The role kind stays `coordinator`; "is the resident" is `task.IsResident()`, so no schema CHECK change.

**Alternative: let the resident call `hera_new_orchestrator` itself.** Rejected: it would be unreliable (depends on the model remembering), would let it name or create several orchestrators, and runs into the existing guard (D6).
**Alternative: one orchestrator per errand.** Rejected: the user's requirement is one standing team, and per-errand orchestrators multiply the rail and the recycle surface.

### D2. Spawning: project is mandatory, spawn is allow-tier

`hera_spawn_worker` for a resident caller SHALL require `project` and SHALL reject an unregistered one, because the resident has no repo to default from. The call is on the **allow** list (decided: never ask). The brakes that replace the prompt are the Argus-sandbox gate (D4a), the backend-enable setting (D4), the project-must-be-registered check, and the `[resident]` orientation. Everything else about spawn is unchanged: `agent.SpawnHeraWorker`, branch namespacing `argus/resident/<role>`, `[backend_routing]` over the enabled families.

### D3. `hera_delete_worker`: exclusive by connection-bound identity

The tool takes `role` (name or id within the resident orchestrator) and optional `force` (default false). The guard has three independent layers, all in the MCP handler:

1. **Caller identity is the `?caller=<taskID>` bound at spawn**, the same mechanism `task_recycle` uses, NOT the self-reported `cwd`/`id` the other hera tools accept. The handler ignores `cwd`/`id` entirely. A call on a connection with no bound caller, or whose caller is not `IsResident()`, fails with an explicit "resident only" error before any lookup. Tool listing hides the tool from non-resident connections (defense in depth; the handler is the enforcement).
2. **Scope:** the target role must belong to the resident's standing orchestrator and be `kind=worker`; the resident's own role (or any role whose task `IsResident()`) is rejected as "never itself".
3. **Not exposed elsewhere:** no REST endpoint, no TUI key. The human path (`POST /api/hera/roles/{id}/nuke`) is unchanged and is not a bypass of the safety default because it is the human's own action.

Execution reuses `hera.NukeSingleRole` (stop session, archive-and-reclaim worktree + branch + task). Because `NukeSingleRole` returns a background closure and does not report refusal, the tool wraps it:

- **Default (force=false):** classify the worker's task with merge-safety first. A shared helper `mergesafety.ClassifyTask(ctx, task)` (extracted from `internal/api/cleanup_candidates.go`'s candidate construction so there is one construction path) returns a Verdict. Proceed only if `Safe`, OR the work is **empty** = no commits ahead of the worker's base ref and a clean `git status --porcelain` in its worktree (empty is checked locally, fails closed). Otherwise refuse with a reason naming the verdict (for example "not safe: no merged PR for branch argus/resident/foo; commits ahead: 3") and change nothing.
- **force=true:** skip classification and nuke. Because nothing prompts, the tool computes an **unmerged summary BEFORE the nuke** and returns it in the tool result: branch name, commits ahead of the base ref (count and subjects, first 10), dirty/untracked file count, and last known PR state. Each field degrades to `unknown (<reason>)` rather than being omitted, and a failure to compute never blocks the force delete. The resident orientation requires it to report every force delete, naming the worker and the unmerged content, in its own conversation (the pane Aaron reads) in the same turn. A test asserts the tool result carries the summary.
- The tool is on the **allow** list (decided: never ask). The two brakes are the merge-safety default and the mandatory force report.

Merge-safety fails closed everywhere (network failure means not safe), so a transient GitHub outage refuses a default delete; the resident can retry or use `force`.

**Alternative: keep delete on the ask tier.** Rejected by decision: an unattended assistant that must wait for a keypress to clean up defeats the point; the force report is the safety net instead.
**Alternative: reuse the REST nuke from the resident.** Rejected: no refusal semantics, and it would put a destructive human endpoint on an agent.
**Alternative: allow any coordinator and gate by config.** Rejected by requirement; exclusivity needs a code guard.

### D4. Workers must never block: three layers, per family

Stuck points for an unattended worker: (i) a permission prompt for a tool/command, (ii) `AskUserQuestion`, (iii) plan-mode entry/exit approval. All three are interactive prompts nobody answers.

**(a) Bypass only when the Argus sandbox wraps the worker.** A resident-spawned worker carries a marker on its task (`task_meta(hera, resident_worker)`, stamped in the same `AfterPersist` hook that stamps the role, so it exists before launch and survives resume). `BuildCmd` for a marked task: if `argusWrapsSandbox(task, cfg)` is false the launch is refused with a "resident worker requires the Argus sandbox" error (mirroring the resident's own "sandbox profile failure fails closed" rule), and `hera_spawn_worker` returns that error to the resident instead of creating a half-started task (a pre-flight check in the spawn path, plus the BuildCmd check as the backstop for resume/restart). If it wraps, the family's bypass-active permission flags are applied regardless of `defaults.permission_mode`. The sandbox is what makes bypass tolerable: file writes confined to the worktree grants, network per the sandbox profile.

Known limit: a user-level managed `disableBypassPermissionsMode` in Claude settings would make the bypass flag inert. The worker does not use `--setting-sources project` (it must keep the user's plugins/skills/hooks like any worker), so this is a user-config risk; layer (c) is the backstop and the spawn logs the effective mode.

**(b) Remove the question tools; redirect through Hera.** A new per-family capability `Family.UnattendedWorker` (in `internal/agent/backends.go`, per the registry rule) declares how that family closes stuck points. Claude: `--disallowedTools AskUserQuestion EnterPlanMode ExitPlanMode` appended to the launch. The worker orientation (added to `HeraWorkerOrientation` for marked workers only) says: you cannot ask the user; if you need a decision, call `hera_send(status=blocked)` to the resident with the question and the options you see, then stop and wait. The resident either answers, relays the question to Aaron in its own pane (it is the user-facing agent), nudges, or force-deletes.

**(c) Backstop.** The existing daemon needs-input detector already flags any live role. A new notification: when a worker in the resident orchestrator is flagged needs-input (detector or self-reported `blocked`), the daemon sends the resident a Hera message through the normal bus: "worker X appears to be waiting on a prompt: <tail excerpt>, options: reply with hera_send, nudge, or hera_delete_worker". It is edge-triggered per flag episode (one message per raise, none while the flag stays up), via the same exactly-once notifier. The detector tick runs in the REST server today (`internal/api/push.go`, which also owns `residentWaiting`); the hook point is where that tick already computes the sticky set.

**Backend families and the enable setting.** A family is **eligible** iff its `Family.UnattendedWorker` capability is non-nil. Eligibility is code (the registry); *enablement* is user config:

- `[resident] worker_backends = ["claude", "codex"]` (default: every eligible family, i.e. Claude and Codex). Settings gets a **Resident → Worker backends** row that lists ONLY eligible families as toggles; an ineligible family never appears, and a name in `config.toml` that is not eligible is ignored with a one-time `[resident]` log line (never silently honored). The usual config.toml-shadows-Settings read-only marker applies. An empty set means resident spawns are refused with "no worker backend enabled".
- Resident-worker routing runs `[backend_routing]` and project preferences over the **enabled** set (same filter pattern as the Claude-only coordinator filter in `internal/agent/hera_spawn.go`); an explicit `backend` outside the enabled set is rejected naming the enabled ones.

| Family | Permission prompts | Question/plan tools | Verdict |
|---|---|---|---|
| claude | bypass-active flag, Argus-sandbox-gated | `--disallowedTools AskUserQuestion EnterPlanMode ExitPlanMode` | eligible |
| codex | default command is already `codex --dangerously-bypass-approvals-and-sandbox`: approvals never prompt, but Codex's own sandbox is OFF, so the Argus sandbox is the only wall and the gate in (a) is essential | mechanism determined by an early spike (below) | **eligible in this change, gated on the spike**: if the spike cannot close the user-input path the family's `UnattendedWorker` stays nil, Codex is not listed in Settings and is filtered from routing, and the change ships Claude-only |
| pi, opencode, agy, cursor, omp, custom | opencode `--auto` approves asks; agy/cursor/omp have mode flags; pi/custom none | no verified disable mechanism | **ineligible** (never listed) |

**Codex mechanism (this change implements it).** The first task of section 4 is a spike on a real Codex session answering, with transcripts recorded in a gotcha: (1) with the bypass command, can the model still raise a user-input prompt (a structured `request_user_input`-style tool, a plan/collaboration-mode choice, an approval escalation)? (2) which launch switch closes each (candidates: an approval policy of `never`, a `-c` feature override that disables the user-input tool, a config-profile that omits it)? (3) does the switch survive `resume`? The implementation then fills `UnattendedWorker` for Codex with whatever the spike proves, appended through the family's own launch-flag seam (never a backend branch outside `backends.go`), and the Argus-sandbox gate applies unchanged. Orientation for Codex workers is the same "ask the resident via `hera_send(status=blocked)`" text. The needs-input backstop (c) covers Codex too.

Rationale: "equivalent mechanism or pin to families where the stuck points can be closed" is satisfied by Claude now, Codex via the spike, and everything else pinned out and invisible. Adding a family later means filling its `UnattendedWorker`, one backends.md row; it then appears in Settings automatically.

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

**Manage, never create** is enforced structurally: resident mode sets a rail-key allowlist in `handleRailMutation`'s gate (`isRailMutationKey` already classifies mutation keys): navigation (move, fold, Enter, `i`, fullscreen toggle, copy, focus) plus the existing **nuke, hide, rename and role-status step** actions, each valid ONLY when the selected row is a worker (inert on the resident coordinator row, so the human cannot nuke, hide or rename the resident from here). Every spawn / new coordinator / move / join / detach / pin / kanban / bounce key is inert in resident mode and absent from the `?` overlay and ctrl+k palette for that context; nuke/hide/rename/status appear there for worker rows. A smoke test asserts each creation/structure key is inert and each allowed one acts on a worker row only. Keymap changes follow `gotchas/keybindings.md`: the allowlist is data in `keymap`, the help rows are generated. The human nuke reuses the existing cascade-free single-role nuke path with its merge-safety confirmation unchanged (it is the human's own action).

**The resident is told.** Whenever a worker of the standing orchestrator is nuked, hidden (archived) or renamed by anything other than the resident's own `hera_delete_worker`, the daemon sends the resident one Hera message through the same daemon-originated path as the needs-input backstop (D4c): "Aaron nuked worker X (project P, branch B; unmerged: ...)", "Aaron hid worker X (its session was stopped)", "Aaron renamed worker X to Y". The hook sits at the shared reclaim/archive/rename choke points the TUI rail, REST and the Tasks-list delete all converge on (so deleting a resident worker from the Tasks list is covered too), with the actor passed down so the resident's own delete is not echoed back. Role-status stepping is visible through `hera_tree_updates` and sends no message. Messages are durable and deliver when the resident is idle.

The existing resident requirements stay: `global.resident` toggle and exact return, rail box, tab-label marker, no size-drift kick on the resident pane.

### D9. Permissions

`[resident]` defaults change as follows; `resident_inventory_test.go` classifies every hera tool so a new one cannot land in allow by accident.

- **Allow:** `hera_send`, `hera_inbox`, `hera_get_messages`, `hera_mark_read`, `hera_status`, `hera_tree_updates`, `hera_accept`, `hera_revive`, `hera_unblock`, `hera_board_list/post/update/resolve`, **`hera_spawn_worker`, `hera_delete_worker`**. Each acts only within the caller's own orchestrator because the resident's connection is bound. Spawn and delete are allow by decision (never ask); their brakes are D2/D3.
- **Ask:** **`hera_graduate_worker`** only. It is the single coordination tool that prompts: the Claude permission prompt IS Aaron's yes, so approval is enforced mechanically and not just by orientation (D11).
- **Deny:** `hera_new_orchestrator`, `hera_join`, `hera_move`, `hera_rebind`, `hera_plan`, `hera_plan_node`, `hera_plan_node_update`, `hera_plan_node_cancel`, `hera_block` (no plan DAG or membership surgery in v1).
- Outward-connector tiers and the existing allow entries are unchanged. `hera_accept` is allow by decision.

Note `task_inbox` / `task_message_ack` stay allowed (resident-to-resident bus for non-Hera senders); Hera workers address the resident through `hera_send` and the resident reads `hera_inbox`. The orientation prompt is updated to say which inbox to read for what.

### D10. What happens to workers when the resident recycles or restarts

Workers are separate supervisor-owned sessions bound to the standing orchestrator; nothing about the resident's session lifecycle stops them.

- **Recycle:** only the resident session is killed and relaunched. The role binding stays live (the row and binding are not ended). Worker messages sent meanwhile are durable in the Hera inbox; doorbells are idle-gated and deliver when the fresh session is idle. The seed's team section (D5) re-orients it.
- **Crash/daemon bounce:** same; the daemon re-attaches the supervisor-owned sessions. The existing startup hera-binding reconciliation and finished-session reaper already spare live coordinator bindings; the resident binding counts as one.
- **`resident.enabled` becomes false:** the resident session stops; its row, binding, orchestrator and the vault are intact; **workers keep running** (stopping them mid-task would lose uncommitted work). Settings shows "N workers still running"; workers that report blocked queue their message. Re-enabling resumes the same binding. This is deliberately lenient; the alternative (stop workers too) is an open question.
- **Prerequisite failure stop** (two consecutive ticks) behaves like disable for workers.

### D11. Worker graduation: a resident worker becomes a stand-alone top-level coordinator

**Flow.** A resident worker decides its job is big (multi-PR or multi-worker scope, needs its own team, will outlive an errand). It asks the resident with `hera_send` (it MUST ask, never self-promote). The resident asks Aaron in its own pane; on yes it calls `hera_graduate_worker(role)`. Aaron can also start it ("make that a stand-alone coord"); the resident then calls the same tool. The tool is on the resident's **ask** list, so the Claude permission prompt is the yes: without it nothing happens, regardless of what the model believes was approved.

**Target shape: a TOP-LEVEL orchestrator in the normal Hera rail, not a sub-coordinator under the resident.** The resident's orchestrator is hidden from `/api/hera` and its tab is a one-coordinator view (D7, D8), so a nested sub-team would be invisible exactly where big work belongs. After graduation the task has no live binding in the resident's orchestrator and no parent link to it.

**Mechanism: one new transactional store method, composed from the existing insert helpers.** I investigated composing the two existing primitives, self-promotion (`hera_new_orchestrator` for the worker's task) followed by `AdoptOps.DetachCoordinator`:

- Self-promotion leaves the worker's own binding live, so the task is dual-bound; the rail shows a nested sub-coordinator until the detach step runs. That window is user-visible and, if the daemon dies between the two steps, permanent.
- `DetachCoordinator` is a TUI-side `AdoptOps` operation over `hera_orch_links` bridge roles (`teardownParentLinks` deletes the parent-link role). It lives in `internal/tui/hera`, is not reachable from the daemon/MCP layer, and tears down a *bridge role*, which a plain worker binding is not (the worker's own role is the live binding to end, not a link to delete).
- `hera_new_orchestrator` carries guards written for a different situation (non-Claude rejection, same-name handling, a coordinating-caller scan) and is denied to resident workers by this change anyway.

So graduation is a new `db.GraduateHeraWorker(workerBindingID, orchName, coordRoleName, prompt)` modeled directly on `MoveHeraBinding`: in ONE transaction it (1) ends the worker binding with end reason `graduated` (new constant beside `HeraEndReasonMoved`), (2) creates the new top-level orchestrator (name derived from the worker role name, de-collided with the existing unique-name helper, never `resident`), (3) inserts a `coordinator` role with the worker role's project and prompt, and (4) inserts its binding to the SAME task and worktree. Mail for the ended worker role is cleared with `clearDeadRoleMailAfterEnd`, as in a move. It never creates an orchestrator link, so there is nothing to detach and no intermediate nested state. Either all four steps commit or none do.

**Guard (same shape as `hera_delete_worker`).** (1) Caller identity only from the connection-bound `?caller=`; ignore `cwd`/`id`; non-resident or unbound callers fail before any lookup; the tool is hidden from non-resident tool lists; no REST or TUI entry point. (2) Scope: the target is a live `worker` of the resident's standing orchestrator and its task is not `IsResident()`. (3) Fail-closed preflights before the transaction: the worker's backend must be coordinator-capable (the same `nonClaudeTaskBackend` rule `hera_new_orchestrator` applies, since a coordinator needs coord-hook context tracking, cost accrual and recycle), so a Codex resident worker CANNOT graduate in v1 and the error says so; the task must have a session id to resume; the worker's session must be idle or absent.

**Resident workers cannot self-promote.** `hera_new_orchestrator` rejects, fail-closed and before any scan, a caller that holds a live worker binding in the standing orchestrator (and, belt and braces, a task carrying the resident-worker marker), with: "you are a resident worker; ask the resident to graduate you (hera_send), do not create an orchestrator yourself". This is the same early-check slot as the resident's own rejection in D6.

**Delete can no longer reach a graduate.** Delete scope is "a live worker of the resident's standing orchestrator". After the transaction the role's binding is ended and the new coordinator role lives in another orchestrator, so the existing role lookup by name inside the standing orchestrator finds no live worker. This falls out of the scope rule with no extra code; a test asserts `hera_delete_worker` on a graduated role is rejected and the task, worktree and branch are untouched. The resident's rail drops the row because the roster is built from live bindings.

**Launch flags: restart in place with resume, normal coordinator flags.** The worker launched with the unattended flags (bypass-active mode, disallowed question/plan tools) keyed on the resident-worker marker (D4). A stand-alone coordinator must behave like a normal coordinator, so after the transaction graduation:

1. clears `task_meta(hera, resident_worker)` and writes `task_meta(hera, graduated_from) = resident` (audit);
2. restarts the session in place with **resume** (same session id, same worktree, same branch, same conversation) using the existing kill-and-resume primitive the size-drift kick and revive use, NOT a recycle (a recycle starts empty-context and would lose the conversation); the next `BuildCmd` sees no marker, so it applies `defaults.permission_mode` and the normal tool set, and the Argus sandbox setting is whatever the task already baked in;
3. runs after the worker session is idle, via a bounded one-shot idle wait like `taskRecycleRunner.awaitIdleAndRestart` (the call comes from the resident, so the worker is normally idle having just asked), with stray-job cleanup first; on timeout the pending restart is recorded in task meta, the coordinator binding already exists, and the next launch picks up the normal flags;
4. once restarted, the daemon sends the new coordinator one orientation message (the standard coordinator orientation plus: you were graduated from the resident's team, you are now a top-level coordinator, the resident is not your coordinator and cannot message you, report to Aaron through the normal channels, dispatch work with `hera_spawn_worker`).

Because the marker is cleared before the restart and the binding is already a coordinator, a crash or daemon bounce at any point still resumes into the right flags; the only transient is that a not-yet-restarted session keeps its old (sandboxed) bypass flags until it idles.

**After graduation** the task is an ordinary top-level coordinator: ordinary recycle (the coordinator path, not the resident runner), ordinary budget, ordinary rail entry and `/api/hera` presence. Its branch keeps the `argus/resident/<role>` name (renaming a live branch under a running agent is not worth the risk).

**Orientation.** The worker orientation says when to ask for graduation (scope likely to need more than one PR or more than one worker, a long-lived program of work, needing to fan out) and that it must ask via `hera_send` rather than self-promote. The resident orientation says to ask Aaron first in plain words (what the job is, why it needs its own team), then call the tool, whose prompt confirms.

**Alternative: nested sub-coordinator under the resident (the existing self-promotion shape).** Rejected: invisible in the normal rail and `/api/hera`, driven through a one-coordinator tab, and the resident would be its coordinator for work too big to supervise.
**Alternative: compose self-promotion + `DetachCoordinator`.** Rejected above: non-atomic, visible intermediate state, and the detach operation is not available to the daemon layer.
**Alternative: graduation by a human-only TUI action.** Rejected: Aaron asked for the resident-initiated flow with his yes enforced; the ask-listed tool gives both without a second code path.
**Alternative: restart with a recycle (empty context + seed).** Rejected: loses the conversation the worker built; resume keeps it and only changes launch flags.
**Alternative: self-reported approval flag on the tool.** Rejected: a model-supplied "approved=true" is not approval; the permission prompt is.

## Alternatives considered

- **A separate "resident team" mechanism** (not Hera): rejected, it would duplicate spawn, bus, merge-safety and the rail.
- **Making the standing orchestrator visible in the Projects tab:** rejected; the Projects tab has creation actions and a second entry point to the same team defeats the navigation-only requirement.
- **Bypass mode for all hera workers:** rejected; only resident-spawned workers are unattended by design, and bypass without the Argus sandbox is unacceptable.
- **A per-spawn backend argument instead of a per-backend enable setting:** rejected; Aaron wants a standing, visible switch, and Settings listing only eligible families makes an unsafe family impossible to turn on.

## Risks / Trade-offs

- Bypass mode plus the sandbox concentrates trust in the sandbox profile; a profile gap becomes unattended arbitrary command execution in a worktree. Mitigation: refuse-if-unwrapped, ask-tier spawn, the existing sandbox gotchas.
- `--disallowedTools` removes the tools but a model may still try to ask in free text and idle. The needs-input backstop (c) and the orientation cover this.
- Merge-safety refusing on a transient network failure can make cleanup feel sticky; `force` is the escape and, like every delete, needs no prompt.
- Delete and spawn are agent-initiated with no prompt (decision). The remaining brakes are the merge-safety default, the mandatory pre-nuke unmerged summary reported in the resident's conversation, the sandbox gate and the enabled-backends set. A runaway resident could spawn many workers; see the cap question.
- Codex eligibility is unverified until the spike; if the spike fails Codex stays unlisted and the change ships Claude-only rather than shipping a hole. The Argus sandbox is the only wall for Codex workers because the default command disables Codex's own.
- Graduation restarts a live session; a mid-turn worker is waited on (idle gate) and a timed-out restart leaves the old flags until the next launch. A graduate on a non-Claude backend is refused outright.
- The human can now nuke or hide a resident worker from the rail; the resident learns only via the daemon message, which is idle-gated, so it can act on a stale picture for a moment.
- Two recycle triggers now converge on one runner: the shared in-flight guard must be exercised by a test where both fire.

## Migration Plan

No data migration. On first start after upgrade the daemon creates the orchestrator, role and binding for an existing resident row. Existing resident sessions pick up the new allow/ask/deny policy and orientation on their next launch (recycle or restart); until then they have no hera tools in their policy, which is harmless.

## Acceptance criteria

- Standing orchestrator exists exactly once, survives resident restarts, and is invisible to Projects/REST roster/cleanup.
- `hera_delete_worker` from any non-resident connection (including a regular coordinator and a resident worker) is rejected; from the resident it deletes only its own workers, refuses itself, and refuses unmerged non-empty work unless `force`.
- A resident worker cannot launch without the Argus sandbox; on Claude it cannot call `AskUserQuestion`/plan tools; an ineligible backend is never picked by routing.
- A needs-input flag on a resident worker produces exactly one message to the resident per episode.
- Resident tab rail has no creation key live; nuke, hide, rename and status act on worker rows only and never on the resident row; the resident receives a message for each human nuke, hide and rename.
- `hera_spawn_worker` and `hera_delete_worker` run with no prompt; a force delete's tool result and the resident's conversation both name the worker and what was unmerged.
- Settings lists only eligible worker backends; a disabled family is never routed or accepted explicitly; Codex workers launch unattended-safe once the spike passes.
- `hera_graduate_worker` prompts, is resident-only, converts a worker into a visible top-level coordinator in one transaction, restarts it with resume and normal coordinator flags without losing worktree, branch or conversation, and afterwards `hera_delete_worker` cannot reach it. A resident worker calling `hera_new_orchestrator` is rejected with the graduation hint.
- A recycle triggered by `task_recycle` and by `recycle_coord` on the resident run the same code path and never overlap; workers are untouched.

## Open Questions

The six earlier questions are answered and folded into the decisions above. New ones:

1. Should resident spawns have a cap (for example `[resident] max_workers`, default 8) now that spawn never asks? Not specified; recommended.
2. Codex workers cannot graduate in v1 (a coordinator needs Claude). Acceptable, or should graduation of a Codex worker respawn it on Claude with a handoff?
3. Should graduation also keep the worker's role name as the coordinator role name and derive the orchestrator name from it (specified), or ask Aaron for a name in the prompt?
4. A graduated coordinator keeps its `argus/resident/<role>` branch name. Acceptable, or rename on graduation?
5. If the post-graduation restart times out waiting for idle, the session keeps its bypass flags until the next launch. Acceptable, or force the restart after the timeout?
