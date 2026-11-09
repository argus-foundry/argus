## ADDED Requirements

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

The resident SHALL spawn workers with `hera_spawn_worker`, which SHALL require `project` for a resident caller and SHALL reject an unregistered project, because the resident has no repository to default from. Spawned workers SHALL be born bound to the resident's orchestrator and SHALL exchange messages with the resident over the Hera bus and inbox. Spawn SHALL use the existing `agent.SpawnHeraWorker` path and branch namespacing under the orchestrator.

#### Scenario: Project required

- **WHEN** the resident calls `hera_spawn_worker` without `project`
- **THEN** the call fails with an error saying a project is required and no task is created

#### Scenario: Unknown project

- **WHEN** the resident names a project that is not registered
- **THEN** the call fails and no task, worktree or role is created

#### Scenario: Worker is on the team

- **WHEN** the resident spawns a worker in a registered project
- **THEN** a role of kind worker bound to the new task exists in orchestrator `resident` and the worker's `hera_send` reaches the resident

### Requirement: hera_delete_worker is exclusive to the resident

The system SHALL provide an MCP tool `hera_delete_worker(role, force?)` usable only by the resident. The handler SHALL identify the caller solely by the connection-bound `caller` set at spawn, SHALL ignore any self-reported `cwd` or `id`, and SHALL fail before any lookup when the connection has no bound caller or the caller is not `IsResident()`. The tool SHALL NOT be listed to non-resident connections and SHALL have no REST endpoint or TUI key. It SHALL delete only a role of kind worker in the resident's own orchestrator and SHALL reject the resident's own role or any role whose task `IsResident()`. Deletion SHALL reuse `hera.NukeSingleRole`: stop the session and remove the worktree, branch and task.

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

### Requirement: Deleting a worker is merge-safe by default and forceable

Unless `force` is true, `hera_delete_worker` SHALL first classify the worker's work with the merge-safety classifier, built through one shared single-task helper that uses the same candidate construction as the cleanup pass, and SHALL proceed only when the verdict is safe or the work is empty. Empty SHALL mean no commits ahead of the worker's base ref and a clean working tree, determined locally and failing closed. Otherwise it SHALL refuse with a reason naming the verdict and SHALL change nothing. With `force` true it SHALL skip the check and the result SHALL state what was destroyed. A classification failure SHALL be treated as not safe.

#### Scenario: Merged work is deleted

- **WHEN** the worker's branch is confirmed merged and `force` is absent
- **THEN** the worker is stopped and its worktree, branch and task are removed

#### Scenario: Empty work is deleted

- **WHEN** the worker has no commits ahead and a clean tree
- **THEN** it is deleted without needing a merged PR

#### Scenario: Unmerged work is refused

- **WHEN** the worker has unmerged commits and `force` is absent
- **THEN** the call is refused with the verdict reason and the worker, worktree and branch are untouched

#### Scenario: Force skips the check

- **WHEN** `force` is true and the worker has unmerged commits
- **THEN** the worker is deleted and the result reports the destroyed branch

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

### Requirement: Resident workers route only to families that close the stuck points

Each backend family SHALL declare in the family registry whether and how it closes the unattended stuck points. Resident-worker routing SHALL filter `[backend_routing]` tiers and project preferences to eligible backends, and an explicit `backend` naming an ineligible family SHALL be rejected with an actionable error. Claude SHALL be eligible. Codex SHALL be eligible only when the Argus sandbox gate applies (its default command disables Codex's own sandbox and approvals) and its structured user-input path is confirmed closed; until confirmed it SHALL be filtered out. Families without a verified mechanism SHALL be ineligible.

#### Scenario: Routing skips ineligible tiers

- **WHEN** `[backend_routing]` ranks an ineligible backend first
- **THEN** a resident worker spawn routes to the best eligible backend instead

#### Scenario: Explicit ineligible backend

- **WHEN** the resident names an ineligible backend explicitly
- **THEN** the spawn is rejected naming the backend and the eligible ones

#### Scenario: No eligible backend

- **WHEN** no configured backend is eligible
- **THEN** the spawn is rejected with that reason and no task is created

### Requirement: The resident's hera tools are tiered

The `[resident]` defaults SHALL allow `hera_send`, `hera_inbox`, `hera_get_messages`, `hera_mark_read`, `hera_status`, `hera_tree_updates`, `hera_accept`, `hera_revive`, `hera_unblock` and the `hera_board_*` tools; SHALL put `hera_spawn_worker` and `hera_delete_worker` on the ask list; and SHALL deny `hera_new_orchestrator`, `hera_join`, `hera_move`, `hera_rebind`, `hera_plan`, `hera_plan_node`, `hera_plan_node_update`, `hera_plan_node_cancel` and `hera_block`. The resident's outward-connector allow, ask and deny tiers SHALL be unchanged. The inventory test SHALL classify every hera tool so a new one cannot enter the allow list unclassified.

#### Scenario: Coordination does not prompt

- **WHEN** the resident calls `hera_send` or `hera_inbox`
- **THEN** no confirmation is requested

#### Scenario: Spawn and delete prompt

- **WHEN** the resident calls `hera_spawn_worker` or `hera_delete_worker`
- **THEN** a confirmation is requested showing the arguments

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

The standing orchestrator, its coordinator role and its binding SHALL be excluded from the Projects tab model, the jump switcher's Hera column, the `/api/hera*` roster, cleanup and merge-safety roll-ups, and cascade-nuke candidate sets, and SHALL NOT be nukeable, hideable, detachable or movable through any generic path. Its workers SHALL remain ordinary tasks in the Tasks list. `TestResidentExcludedEverywhere` SHALL cover these choke points.

#### Scenario: Not in the Projects tab

- **WHEN** the resident team exists and the Projects tab or `/api/hera` roster is read
- **THEN** the standing orchestrator does not appear

#### Scenario: Generic sweeps skip it

- **WHEN** a cleanup pass or cascade nuke enumerates candidates
- **THEN** the resident's orchestrator, role and binding are not among them

#### Scenario: Workers are normal tasks

- **WHEN** a resident worker is running
- **THEN** it appears in the Tasks list like any hera-managed task
