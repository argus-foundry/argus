# resident-coordinator Specification

## Purpose
TBD - created by archiving change add-resident-coordinator. Update Purpose after archive.
## Requirements
### Requirement: The resident coordinates one standing orchestrator

The system SHALL give the resident exactly one Hera orchestrator, named `resident`, with one `coordinator` role named `resident` bound to the resident task. The daemon SHALL create the orchestrator, role and binding idempotently in one transaction the first time the resident starts, before its session launches, and SHALL NOT end the binding on resident recycle, crash, disable or daemon restart. The orchestrator SHALL NOT be created through `hera_new_orchestrator`. The name `resident` SHALL be reserved: creating any other orchestrator with that name SHALL be rejected, and if a user orchestrator already holds it the resident SHALL NOT start and the resident state reason SHALL say so. Whether a role is the resident SHALL be decided by `task.IsResident()`, not by a new role kind.

#### Scenario: First start creates the team

- **WHEN** the resident starts for the first time
- **THEN** orchestrator `resident` with coordinator role `resident` bound to the resident task exists before the session launches

#### Scenario: Restart reuses it

- **WHEN** the resident is recycled, crashes, is disabled and re-enabled, or the daemon restarts
- **THEN** the same orchestrator, role and binding are used and no second one is created

#### Scenario: Renaming does not touch Hera rows

- **WHEN** the user changes `resident.name`
- **THEN** the orchestrator and role names are unchanged

#### Scenario: Reserved name

- **WHEN** any caller other than the daemon's resident flow creates an orchestrator named `resident`
- **THEN** the request is rejected and nothing is created

#### Scenario: Name already taken

- **WHEN** a user orchestrator named `resident` exists before the resident first starts
- **THEN** the resident does not start and the reported reason names the collision

### Requirement: The resident spawns workers into its orchestrator with an explicit project

The resident SHALL spawn workers with `hera_spawn_worker`, which SHALL require `project` for a resident caller and SHALL reject an unregistered project, because the resident has no repository to default from. Spawned workers SHALL be born bound to the resident's orchestrator and SHALL exchange messages with the resident over the Hera bus and inbox. Spawn SHALL use the existing `agent.SpawnHeraWorker` path and branch namespacing under the orchestrator. Spawning SHALL NOT require a confirmation prompt. Spawning SHALL be bounded by `[resident] max_workers` (default 8, a value below 1 treated as the default), counted as the standing orchestrator's live worker bindings so that graduated, deleted and ended workers do not count; at the cap the call SHALL refuse before creating anything and the error SHALL state the cap and the current live count. The setting SHALL be editable in Settings → Resident and in `config.toml`, with `config.toml` values shown read-only with the override marker.

#### Scenario: Project required

- **WHEN** the resident calls `hera_spawn_worker` without `project`
- **THEN** the call fails with an error saying a project is required and no task is created

#### Scenario: Unknown project

- **WHEN** the resident names a project that is not registered
- **THEN** the call fails and no task, worktree or role is created

#### Scenario: Cap reached

- **WHEN** the resident has 8 live workers with the default cap and calls `hera_spawn_worker`
- **THEN** the call is refused with the cap and the live count and no task is created

#### Scenario: Freed slots reopen the cap

- **WHEN** one worker is deleted or graduated at the cap
- **THEN** the next spawn succeeds

#### Scenario: Configurable cap

- **WHEN** `max_workers` is set to 3 in Settings and three workers are live
- **THEN** a fourth spawn is refused naming a cap of 3

#### Scenario: Worker is on the team

- **WHEN** the resident spawns a worker in a registered project
- **THEN** a role of kind worker bound to the new task exists in orchestrator `resident` and the worker's `hera_send` reaches the resident

### Requirement: hera_delete_worker is exclusive to the resident

The system SHALL provide an MCP tool `hera_delete_worker(role, force?)` usable only by the resident. The handler SHALL identify the caller solely by the verified connection-bound `caller` set at spawn (caller id plus per-launch token), SHALL ignore any self-reported `cwd` or `id`, and SHALL fail before any lookup when the connection has no bound caller or the caller is not `IsResident()`. The tool SHALL NOT be listed to non-resident connections and SHALL have no REST endpoint or TUI key. It SHALL delete only a role of kind worker in the resident's own orchestrator and SHALL reject the resident's own role or any role whose task `IsResident()`. Deletion SHALL reuse `hera.NukeSingleRole`: stop the session and remove the worktree, branch and task.

#### Scenario: Regular coordinator is refused

- **WHEN** a non-resident coordinator, worker or freelance calls `hera_delete_worker`
- **THEN** the call fails with a resident-only error and nothing is deleted

#### Scenario: Self-reported identity is ignored

- **WHEN** a non-resident connection passes the resident's `cwd` or task id in the arguments
- **THEN** the call is still refused

#### Scenario: Only own workers

- **WHEN** the resident names a role in another orchestrator
- **THEN** the call fails and nothing is deleted

#### Scenario: Never itself

- **WHEN** the resident names its own role
- **THEN** the call fails and the resident is unchanged

#### Scenario: Not listed to others

- **WHEN** a non-resident connection lists tools
- **THEN** `hera_delete_worker` is absent

### Requirement: The resident's connection identity is a per-launch secret

The connection-bound `caller` that identifies the resident SHALL NOT be self-asserted: every resident launch (start, resume, restart, kick or recycle) SHALL generate a fresh unguessable token, deliver it to the resident's Argus MCP connection only through a private launch file under `~/.argus/resident-launch` that is readable by no other sandboxed task and never through the command line or environment, and persist only its hash. The MCP server SHALL treat a connection as bound to the resident only when its `caller` names the resident AND its token matches the current hash; any other connection, including one presenting the resident's task id with a missing, wrong or superseded token, SHALL be treated as unbound. The token SHALL never be logged, listed or returned by any tool, REST endpoint or Hera read. Every resident-only behavior keyed on the bound caller (`hera_delete_worker`, `hera_graduate_worker`, the resident's own `task_recycle`, the resident's Hera argument pinning and tool listing) SHALL use this verified identity. Any tool call from a connection that is not the verified resident whose arguments identify the resident (its `cwd`, or its task id as `id`, `task_id` or `caller_id`) SHALL be refused before dispatch. Built-in tool argument names SHALL be canonical lower-case (`[a-z0-9_]`), and a call carrying any other top-level argument name SHALL be refused, so a case-folded key cannot slip past the identity checks while still reaching the tool.

#### Scenario: Spoofed caller id is refused

- **WHEN** a process posts to the MCP endpoint with `caller` set to the resident's task id and no token, or a wrong token
- **THEN** `hera_graduate_worker` and `hera_delete_worker` (including with `force`) are refused as resident-only and neither tool is listed

#### Scenario: Token rotates on every launch

- **WHEN** the resident is recycled or restarted
- **THEN** the new session's token is accepted and the previous session's token is refused

#### Scenario: Resident cwd is not an identity

- **WHEN** a connection that is not the verified resident calls `hera_spawn_worker`, `hera_status`, `hera_send` or any other tool with the resident's `cwd`
- **THEN** the call is refused and nothing is spawned, stamped or sent

#### Scenario: Case-folded keys are refused

- **WHEN** any connection calls a built-in tool with an argument named `CWD`, `Caller_ID`, `ID` or another non-canonical spelling
- **THEN** the call is refused before dispatch and nothing is created, sent or changed

#### Scenario: Resident task id is not an identity

- **WHEN** a connection that is not the verified resident calls `task_stop`, `task_rename`, `task_complete`, `task_archive` or any other tool with the resident's task id as `id`, `task_id` or `caller_id`
- **THEN** the call is refused and the resident is unchanged

#### Scenario: Token never surfaces

- **WHEN** the resident launches
- **THEN** its token appears in no process argument, environment variable, log line, tool result or API response

### Requirement: Deleting a worker is merge-safe by default and forceable

Unless `force` is true, `hera_delete_worker` SHALL first classify the worker's work with the merge-safety classifier, built through one shared single-task helper that uses the same candidate construction as the cleanup pass, and SHALL proceed only when the working tree is clean and either the verdict is safe or the work is empty; a dirty working tree SHALL refuse without `force` even when the verdict is safe. Without `force` it SHALL also refuse, changing nothing, unless the worker's session is idle or absent ("worker is busy; wait, or force"), and it SHALL proceed in this order: idle check, clean-tree check, classification, stop the session, re-check the tree, then nuke the role and remove the worktree. If the post-stop re-check finds the tree dirty, or the worker's branch tip has moved since classification, it SHALL refuse before nuking, keeping the role, binding and worktree and naming the dirty files; this is the one refusal that leaves a side effect, the stopped session, which a later resume recovers. The whole call, including time spent waiting for any other resident worker mutation, SHALL run under one overall deadline that keeps it inside the MCP client deadline; if before the stop the remaining budget cannot cover the rest, it SHALL refuse with "host busy; retry" and change nothing. Deleting the worker's remote branch SHALL happen asynchronously after the result, with its own timeout and a logged outcome, so the part after the stop is local and bounded. Empty SHALL mean no commits ahead of the worker's base ref and a clean working tree, determined locally and failing closed. Otherwise it SHALL refuse with a reason naming the verdict and SHALL change nothing. With `force` true it SHALL skip the classification, but it SHALL still compute an unmerged summary BEFORE deleting, and the result SHALL be returned once the role-scoped stop and reclaim are done, without waiting for any fleet-wide reclaim reconciliation, which SHALL run asynchronously and log its outcome. The summary SHALL list (branch, commits ahead with subjects, dirty file count, last known PR state; any field it cannot determine SHALL read `unknown (reason)` and SHALL NOT block the delete) and the tool result SHALL carry that summary. The resident's orientation SHALL require it to report every force delete, naming the worker and what was unmerged, in its own conversation. A classification failure SHALL be treated as not safe. Deleting SHALL NOT require a confirmation prompt.

#### Scenario: Merged work is deleted

- **WHEN** the worker's branch is confirmed merged and `force` is absent
- **THEN** the worker is stopped and its worktree, branch and task are removed

#### Scenario: Empty work is deleted

- **WHEN** the worker has no commits ahead and a clean tree
- **THEN** it is deleted without needing a merged PR

#### Scenario: Uncommitted work is refused even when the branch is safe

- **WHEN** the worker's branch classifies as safe (merged, or no commits ahead) but its worktree has uncommitted or untracked files, and `force` is absent
- **THEN** the call is refused naming the dirty file count and the worker, worktree and branch are untouched

#### Scenario: Busy host refuses before stopping

- **WHEN** waiting for another resident mutation or slow session queries leaves too little of the call's budget before the stop
- **THEN** the call is refused with "host busy; retry" and nothing is stopped or deleted

#### Scenario: Busy worker is refused

- **WHEN** the worker's session is busy and `force` is absent
- **THEN** the call is refused with "worker is busy; wait, or force" and nothing changes

#### Scenario: Work written during classification is kept

- **WHEN** an idle worker's tree is clean at the check but becomes dirty, or gains a commit, before its session is stopped, and `force` is absent
- **THEN** the call is refused after the stop, naming the dirty files, and the role, binding and worktree are intact with the session left stopped

#### Scenario: Unmerged work is refused

- **WHEN** the worker has unmerged commits and `force` is absent
- **THEN** the call is refused with the verdict reason and the worker, worktree and branch are untouched

#### Scenario: Force skips the check

- **WHEN** `force` is true and the worker has unmerged commits
- **THEN** the worker is deleted and the result reports the destroyed branch

#### Scenario: Force result carries the unmerged summary

- **WHEN** `force` is true and the worker has three unmerged commits and two dirty files
- **THEN** the tool result names the worker and branch, lists the three commit subjects and the dirty-file count, and the worker is deleted

#### Scenario: Summary failure does not block

- **WHEN** `force` is true and the commit count cannot be computed
- **THEN** the result shows `unknown (reason)` for it and the worker is still deleted

#### Scenario: Summary does not wait on fleet reconciliation

- **WHEN** `force` is true and the fleet-wide reclaim reconciliation is slow
- **THEN** the tool result with the summary returns as soon as the worker's own stop and reclaim finish, and the reconciliation completes in the background with its outcome logged

#### Scenario: No prompt

- **WHEN** the resident calls `hera_delete_worker`
- **THEN** no confirmation is requested

#### Scenario: Classifier failure fails closed

- **WHEN** the merge-safety lookup fails for a network reason and `force` is absent
- **THEN** the delete is refused as not safe

### Requirement: Resident workers never block on a prompt

A worker spawned by the resident SHALL be marked as a resident worker in task meta before its session launches, and SHALL be launched so that no permission prompt, `AskUserQuestion`, `EnterPlanMode` or `ExitPlanMode` approval can block it. (a) The launch SHALL apply the family's bypass-active permission mode, and ONLY when the Argus sandbox actually wraps the launch; otherwise the spawn SHALL be refused with an error returned to the resident, and a later resume or restart that cannot be wrapped SHALL be refused likewise. (b) Where the family supports it the launch SHALL disallow `AskUserQuestion`, `EnterPlanMode` and `ExitPlanMode`, and the worker's orientation SHALL tell it to ask the resident through `hera_send(status=blocked)` instead of asking the user. (c) When a resident worker is flagged needs-input by the detector or reports `blocked`, the daemon SHALL send the resident one message per flag episode naming the worker and the available actions.

#### Scenario: Sandbox gate on spawn

- **WHEN** the resident spawns a worker and the Argus sandbox would not wrap it
- **THEN** the spawn is refused with a sandbox-required error and no task is created

#### Scenario: Sandbox gate on restart

- **WHEN** a resident worker is restarted in an environment where the sandbox cannot wrap it
- **THEN** the launch is refused and the reason is logged

#### Scenario: Bypass applies only when wrapped

- **WHEN** the sandbox wraps a Claude resident worker
- **THEN** its launch carries the bypass-active permission flags regardless of `defaults.permission_mode`

#### Scenario: Question and plan tools removed

- **WHEN** a Claude resident worker is launched
- **THEN** `AskUserQuestion`, `EnterPlanMode` and `ExitPlanMode` are disallowed and its orientation directs questions to the resident

#### Scenario: Ordinary workers unaffected

- **WHEN** a worker is spawned by a regular coordinator
- **THEN** it launches with the normal permission mode and tool set

#### Scenario: Backstop notifies once

- **WHEN** a resident worker is flagged needs-input and the flag stays raised across several ticks
- **THEN** the resident receives exactly one message for that episode, and a new one only after the flag clears and raises again

### Requirement: Resident workers spawn only on enabled accounts whose family closes the stuck points

Each backend family SHALL declare in the family registry whether and how it closes the unattended stuck points; a family that declares a mechanism is eligible. Claude SHALL be eligible. Codex 0.167.0 SHALL remain ineligible because the verification spike could not close its question and plan prompts across resume; its `UnattendedWorker` SHALL remain nil. Families without a verified mechanism SHALL be ineligible. Config `[resident] worker_accounts` SHALL map an agent account name to an enabled flag; an account absent from the map SHALL be enabled. Settings SHALL expose it as a Resident list of per-account toggles that lists ONLY accounts supporting at least one eligible family; an account that supports only ineligible families SHALL never appear, and an unknown or ineligible name in `config.toml` SHALL be ignored with a log line. Resident-worker spawning SHALL choose among (family, account) pairs whose family is eligible, whose account is enabled and supports the family, ranking them with the `[backend_routing]` tiers and per-account usage headroom so work spreads across accounts. An explicit `backend` or `account` outside that set SHALL be rejected naming what is enabled, and an empty set SHALL fail the spawn with a reason. Disabling or removing an account SHALL affect only new spawns and SHALL NOT alter, stop or re-account any running worker or the account its resume uses. The Settings row SHALL be a local-only edit, and keys defined in `config.toml` SHALL be shown read-only with the config.toml override marker.

#### Scenario: Settings lists eligible accounts only

- **WHEN** the user opens the Resident worker-accounts setting with accounts `work` (Claude), `personal` (Codex only) and `legacy` (only an ineligible family)
- **THEN** `work` is listed as a toggle and `personal` and `legacy` do not appear (Codex 0.167.0 failed the unattended spike)

#### Scenario: Hidden then unhidden worker restarts safely

- **WHEN** a resident worker is hidden and unhidden, clearing its task meta, then restarted
- **THEN** live standing-team membership still enforces unattended flags and the sandbox gate through `db.IsResidentWorker`

#### Scenario: Default enables every eligible account

- **WHEN** `worker_accounts` is unset, or a new account is added later
- **THEN** every eligible account, including the new one, is enabled

#### Scenario: Tiers rank pairs without disabling accounts

- **WHEN** a backend's configured tiers name only one account but another enabled account supports that eligible family
- **THEN** the other account remains a candidate and may be selected explicitly; only `worker_accounts` disables it

#### Scenario: Automatic choices retain routing provenance

- **WHEN** resident routing selects the worker's account automatically rather than through an explicit account argument
- **THEN** task creation preserves routed-account provenance, so later graduation does not turn that automatic choice into a human account pin

#### Scenario: Spawn spreads across enabled accounts

- **WHEN** two enabled accounts both support an eligible family and one has much less headroom
- **THEN** the spawn picks the one with more headroom within the `[backend_routing]` ordering

#### Scenario: Disabled account is never used

- **WHEN** an account is disabled
- **THEN** no new resident worker is routed to it and an explicit request for it is rejected naming the enabled accounts

#### Scenario: Running workers are unaffected

- **WHEN** an account is disabled or removed from config while a worker spawned on it is running
- **THEN** the worker keeps running and a later resume or restart of it still uses its own recorded account

#### Scenario: Nothing eligible

- **WHEN** every eligible account is disabled, or no account supports an eligible family
- **THEN** the spawn is rejected with a reason saying which, and no task is created

#### Scenario: Ineligible or unknown name in config.toml

- **WHEN** `config.toml` toggles an account that is unknown or supports no eligible family
- **THEN** it is ignored with a log line and never routed to

#### Scenario: config.toml override marker

- **WHEN** `config.toml` defines `worker_accounts.work = false`
- **THEN** the Settings toggle for `work` is read-only with the override marker and other accounts stay editable

#### Scenario: Remote mode is inert

- **WHEN** the TUI runs with `--remote`
- **THEN** the worker-accounts toggles cannot be edited

#### Scenario: Codex gated on the spike

- **WHEN** the verification spike cannot close Codex's user-input path
- **THEN** an account carrying only Codex is not listed, and no Codex pair is routed to

#### Scenario: Codex worker is unattended-safe

- **WHEN** Codex is eligible, its account is enabled and the sandbox wraps the launch
- **THEN** a Codex resident worker launches with its family's unattended launch switches, and without the sandbox the launch is refused

### Requirement: The resident's hera tools are tiered

The `[resident]` defaults SHALL allow `hera_send`, `hera_inbox`, `hera_get_messages`, `hera_mark_read`, `hera_status`, `hera_tree_updates`, `hera_accept`, `hera_revive`, `hera_unblock`, the `hera_board_*` tools, `hera_spawn_worker` and `hera_delete_worker`; SHALL put `hera_graduate_worker` on the ask list as the only coordination tool that prompts; and SHALL deny `hera_new_orchestrator`, `hera_join`, `hera_move`, `hera_rebind`, `hera_plan`, `hera_plan_node`, `hera_plan_node_update`, `hera_plan_node_cancel` and `hera_block`. The resident's outward-connector allow, ask and deny tiers SHALL be unchanged. The inventory test SHALL classify every hera tool so a new one cannot enter the allow list unclassified.

#### Scenario: Coordination does not prompt

- **WHEN** the resident calls `hera_send`, `hera_inbox` or `hera_accept`
- **THEN** no confirmation is requested

#### Scenario: Spawn and delete do not prompt

- **WHEN** the resident calls `hera_spawn_worker` or `hera_delete_worker`, with or without `force`
- **THEN** no confirmation is requested

#### Scenario: Graduation prompts

- **WHEN** the resident calls `hera_graduate_worker`
- **THEN** a confirmation is requested showing the worker, and nothing happens if it is declined

#### Scenario: Plan and membership tools denied

- **WHEN** the resident calls `hera_plan_node` or `hera_join`
- **THEN** the call is refused

#### Scenario: Connector tiers unchanged

- **WHEN** the resident attempts `slack_send_message`
- **THEN** it is denied as before

### Requirement: The resident keeps its own recycle and the team survives it

The resident SHALL use one recycle mechanism, the resident recycle runner (see the `task-recycle` delta). Recycling, crashing, restarting or disabling the resident SHALL NOT stop, end or modify any worker, role or binding of its orchestrator. Messages sent to the resident while it is down SHALL remain durable in the Hera inbox and be delivered through the normal idle-gated doorbell once it is idle. Disabling the resident SHALL leave workers running and Settings SHALL show how many remain.

#### Scenario: Recycle leaves workers alone

- **WHEN** the resident recycles while three workers are live
- **THEN** the three worker sessions keep running and the resident's binding stays live

#### Scenario: Messages wait for the fresh session

- **WHEN** a worker sends a message while the resident is between sessions
- **THEN** the message is stored and delivered once the fresh session is idle

#### Scenario: Disable keeps the team

- **WHEN** `resident.enabled` becomes false while workers are running
- **THEN** the resident session stops, the workers keep running, and Settings shows the running-worker count

### Requirement: The standing orchestrator is hidden from generic Hera surfaces

The standing orchestrator, its coordinator role and its binding SHALL be excluded from the Projects tab model, the jump switcher's Hera column, the `/api/hera*` roster, cleanup and merge-safety roll-ups, and cascade-nuke candidate sets, and SHALL NOT be nukeable, hideable, detachable or movable through any generic path. No binding SHALL be inserted into the standing orchestrator except the resident's own and a worker created by the resident's `hera_spawn_worker`, so `hera_join`, `hera_move`, `hera_rebind`, adoption and plan materialization into it SHALL be refused; and no live worker binding SHALL be moved out of it except by graduation. Its workers SHALL remain ordinary tasks in the Tasks list. `TestResidentExcludedEverywhere` SHALL cover these choke points.

#### Scenario: Not in the Projects tab

- **WHEN** the resident team exists and the Projects tab or `/api/hera` roster is read
- **THEN** the standing orchestrator does not appear

#### Scenario: Generic sweeps skip it

- **WHEN** a cleanup pass or cascade nuke enumerates candidates
- **THEN** the resident's orchestrator, role and binding are not among them

#### Scenario: No enrollment by join or move

- **WHEN** any task calls `hera_join` or `hera_move` targeting orchestrator `resident`, or a resident worker calls `hera_move` to leave it
- **THEN** the call is refused, no binding changes, and the task does not become a resident worker

#### Scenario: Workers are normal tasks

- **WHEN** a resident worker is running
- **THEN** it appears in the Tasks list like any hera-managed task

### Requirement: The human manages resident workers but never creates them, and the resident is told

The resident tab's rail SHALL allow the human the existing nuke, hide, rename and role-status actions on worker rows and SHALL offer no creation action (spawn, new coordinator, move, join, detach); these actions SHALL be inert on the resident's own row. When a worker of the standing orchestrator is nuked, hidden or renamed by anything other than the resident's own `hera_delete_worker` (the resident rail, REST, or a Tasks-list delete), the daemon SHALL send the resident one Hera message naming the worker and the action, including what was unmerged for a nuke where cheaply known. The resident's own deletes SHALL NOT be echoed back. Role-status stepping SHALL send no message.

#### Scenario: Human nukes a worker

- **WHEN** the human nukes a resident worker from the rail
- **THEN** the resident receives a message naming the worker and that it was nuked

#### Scenario: Human hides a worker

- **WHEN** the human hides a resident worker
- **THEN** the resident receives a message saying it was hidden and its session stopped

#### Scenario: Human renames a worker

- **WHEN** the human renames a resident worker
- **THEN** the resident receives a message with the old and new names

#### Scenario: Tasks-list delete is covered

- **WHEN** the human deletes a resident worker's task from the Tasks list
- **THEN** the resident receives the same nuke message

#### Scenario: Resident's own delete is not echoed

- **WHEN** the resident deletes a worker with `hera_delete_worker`
- **THEN** no human-action message is sent to it

#### Scenario: Resident row is protected

- **WHEN** the human presses nuke, hide or rename with the resident row selected
- **THEN** nothing happens

#### Scenario: Status stepping is silent

- **WHEN** the human steps a worker's role status
- **THEN** no message is sent to the resident

### Requirement: hera_graduate_worker turns a resident worker into a stand-alone coordinator

The system SHALL provide an MCP tool `hera_graduate_worker(role)` usable only by the resident, guarded exactly like `hera_delete_worker`: the caller SHALL be identified solely by the verified connection-bound `caller` (caller id plus per-launch token), `cwd` and `id` SHALL be ignored, a non-resident or unbound caller SHALL fail before any lookup, the tool SHALL be hidden from non-resident tool listings and SHALL have no REST endpoint or TUI key. The target SHALL be a live worker of the resident's standing orchestrator whose task is not `IsResident()`. Before changing anything the tool SHALL refuse, fail-closed, when the worker's backend is not coordinator-capable, when it has no session id to resume, or when its session is not idle. The change SHALL be a single database transaction that ends the worker binding with end reason `graduated`, creates a new top-level orchestrator (named from the worker role name, de-collided, never `resident`), creates a `coordinator` role carrying the worker's project and prompt, and binds it to the same task and worktree; it SHALL create no orchestrator link and SHALL leave either all or none of those changes applied. The graduated coordinator SHALL appear in the normal Hera rail and `/api/hera`, SHALL NOT be nested under the resident, and SHALL use the ordinary coordinator recycle and budget.

#### Scenario: Graduation produces a visible top-level coordinator

- **WHEN** the resident graduates a live Claude worker
- **THEN** a new top-level orchestrator with a coordinator role bound to the worker's task and worktree is visible in the normal Hera rail, the worker binding is ended with reason `graduated`, and no link to the resident's orchestrator exists

#### Scenario: Naming and branch

- **WHEN** a worker with role name `payments-rework` on branch `argus/resident/payments-rework` is graduated
- **THEN** the new orchestrator is named from `payments-rework` (de-collided, never `resident`), the coordinator role keeps that name, the branch name is unchanged, and the existing rail rename can rename the orchestrator

#### Scenario: Atomic

- **WHEN** creating the coordinator role fails after the worker binding would have been ended
- **THEN** the worker binding is still live and no orchestrator, role or binding was created

#### Scenario: Non-resident refused

- **WHEN** a regular coordinator, a resident worker, or a connection passing the resident's `cwd` calls `hera_graduate_worker`
- **THEN** the call fails with a resident-only error and nothing changes

#### Scenario: Not listed to others

- **WHEN** a non-resident connection lists tools
- **THEN** `hera_graduate_worker` is absent

#### Scenario: Other orchestrator's worker or the resident itself

- **WHEN** the resident names a role in another orchestrator, or its own role
- **THEN** the call fails and nothing changes

#### Scenario: Non-Claude worker refused with an offer

- **WHEN** the target worker runs on a backend that is not coordinator-capable
- **THEN** the call is refused before any change, saying only Claude workers can graduate, and the resident's orientation directs it to tell Aaron and offer a fresh Claude worker seeded with a handoff from the worker, graduating that one only on Aaron's yes and never respawning automatically

#### Scenario: Busy or unresumable worker refused

- **WHEN** the worker's session is mid-turn, or the task has no session id
- **THEN** the call is refused before any change with that reason

#### Scenario: Prompt is the approval

- **WHEN** the user declines the permission prompt for `hera_graduate_worker`
- **THEN** the worker is unchanged

### Requirement: Graduation restarts the worker in place with normal coordinator flags

After a graduation commits, the system SHALL clear the task's resident-worker marker, record the origin in task meta, and restart the session in place with resume (same session, worktree, branch and conversation) using the existing kill-and-resume primitive, not a recycle, once the session is idle, never forcing it, with stray background jobs stopped immediately before the restart. The restarted launch SHALL NOT carry the unattended-worker flags (bypass-active mode and disallowed question/plan tools) and SHALL use the normal permission mode and tool set, with the task's existing sandbox setting unchanged. After the restart the daemon SHALL send the new coordinator one orientation message stating it was graduated, is now a top-level coordinator, and is no longer reachable by the resident. The daemon SHALL keep retrying until the session is idle with no deadline, SHALL mark the role as "graduation pending restart" (shown on the rail) for the whole wait, SHALL persist that state so a daemon restart resumes the retry, and SHALL clear the mark when the restart happens.

#### Scenario: Resume keeps the work

- **WHEN** a graduated worker is restarted
- **THEN** it resumes the same session in the same worktree and branch and its conversation history is intact

#### Scenario: Normal flags after restart

- **WHEN** the graduate relaunches
- **THEN** its command line has no unattended bypass flags or disallowed question/plan tools and uses the configured permission mode

#### Scenario: Not a recycle

- **WHEN** graduation restarts the session
- **THEN** no empty-context seed prompt is used

#### Scenario: Waits for idle with no deadline

- **WHEN** the worker session stays busy for an hour after graduation
- **THEN** it is not restarted, no background job is killed, and the daemon keeps retrying

#### Scenario: Pending state is visible

- **WHEN** the restart is waiting for idle
- **THEN** the role shows "graduation pending restart" on the rail and the marker clears once the restart happens

#### Scenario: Retry survives a daemon restart

- **WHEN** the daemon restarts while a graduation restart is pending
- **THEN** the retry resumes on startup and the session still ends up with the normal flags

#### Scenario: Coordinator oriented

- **WHEN** the graduate is running again
- **THEN** it receives one orientation message explaining its new role

### Requirement: Resident workers cannot self-promote and ask for graduation instead

`hera_new_orchestrator` SHALL reject, fail-closed and before its other checks, any caller that holds a live worker binding in the standing orchestrator or whose task carries the resident-worker marker, with a message telling it to ask the resident to graduate it. The resident-worker orientation SHALL tell the worker when to ask for graduation (scope likely to need more than one PR or more than one worker, or a long-running program of work) and that it MUST ask the resident through `hera_send` rather than self-promote. The resident orientation SHALL tell it to ask Aaron in plain words before calling `hera_graduate_worker`, and to start the flow itself when Aaron asks to make a worker a stand-alone coordinator.

#### Scenario: Worker tries to self-promote

- **WHEN** a resident worker calls `hera_new_orchestrator`
- **THEN** the call is rejected with the ask-the-resident hint and no orchestrator is created

#### Scenario: Worker asks

- **WHEN** a resident worker judges its job to be multi-PR
- **THEN** its orientation directs it to `hera_send` the resident a graduation request

#### Scenario: Aaron initiates

- **WHEN** Aaron tells the resident to make a worker a stand-alone coordinator
- **THEN** the resident calls `hera_graduate_worker` and the permission prompt confirms it

### Requirement: A graduate cannot be archived or deleted while its restart is pending

While a task carries the graduation-pending-restart state, archiving or deleting that task SHALL be refused at the database choke point, so every surface that can archive or delete it (the Tasks list and Projects rail in the TUI, REST, MCP, cascade nuke and cleanup) SHALL refuse and SHALL show or return a notice that the graduation restart is still pending. The refusal SHALL change nothing; a cascade nuke over a subtree containing a pending graduate SHALL refuse as a whole before any side effect. Once the restart has happened and the state is cleared, archive and delete SHALL work normally. While the state is set, `hera_join`, `hera_move` and adoption SHALL be refused for that task, the restart watcher SHALL track the graduated coordinator binding recorded at graduation, and the state SHALL be cleared (with a log line) if that binding has ended. The only other way to clear it SHALL be a human-only escape: deleting or archiving a pending graduate from the TUI SHALL offer, behind an explicit second confirmation, to abandon the pending restart, which clears the state, logs it, and then proceeds. That escape SHALL NOT be reachable through MCP; REST SHALL keep refusing. While the state is set, no surface other than the restart watcher SHALL kill and resume the graduate's session: TUI forced re-renders and size-drift kicks (Projects, resident and Tasks views) and the REST resize kick SHALL skip it.

#### Scenario: Archive refused while pending

- **WHEN** the human archives a graduate from the Tasks list, or a REST or MCP client archives it, before its graduation restart has run
- **THEN** the archive is refused with a "graduation restart pending" notice and the task, its meta and its session are unchanged

#### Scenario: Delete refused while pending

- **WHEN** any surface deletes the graduate's task before its graduation restart has run
- **THEN** the delete is refused with the same notice and the worktree, branch and task are untouched

#### Scenario: Cascade nuke refuses as a whole

- **WHEN** the human cascade-nukes an orchestrator subtree that contains a pending graduate
- **THEN** nothing in the subtree is nuked or archived and the pending notice is shown

#### Scenario: Wedge causes are refused

- **WHEN** a pending graduate's session calls `hera_join` or `hera_move`, or the human adopts it
- **THEN** the call is refused and the graduate keeps exactly its one coordinator binding

#### Scenario: Ended graduate binding clears the state

- **WHEN** the graduated coordinator binding recorded at graduation has ended before the restart ran
- **THEN** the watcher clears the pending state with a log line instead of retrying forever

#### Scenario: Only the watcher restarts a pending graduate

- **WHEN** the human presses the re-render key on, resizes, or a web client resizes the view of a pending graduate's session
- **THEN** no kill-and-resume happens and the graduation watcher remains the only restart path

#### Scenario: Human abandon escape

- **WHEN** the human deletes or archives a pending graduate in the TUI and confirms twice that the pending restart should be abandoned
- **THEN** the pending state is cleared and logged and the delete or archive proceeds; no MCP tool offers this

#### Scenario: Normal after restart

- **WHEN** the graduation restart has completed
- **THEN** the graduate can be archived or deleted like any coordinator task

### Requirement: A graduated worker is unreachable by the resident's delete

`hera_delete_worker` SHALL resolve its target only among live workers of the resident's standing orchestrator, so that a graduated role, whose worker binding is ended and whose coordinator role lives in another orchestrator, SHALL be rejected and its task, worktree and branch SHALL be untouched.

#### Scenario: Delete after graduation

- **WHEN** the resident calls `hera_delete_worker` on a worker it already graduated
- **THEN** the call is rejected as not a worker of its orchestrator and nothing is deleted

#### Scenario: Roster drops the graduate

- **WHEN** a worker is graduated
- **THEN** it no longer appears on the resident's rail
