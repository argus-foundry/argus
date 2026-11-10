# task-recycle Specification

## Purpose

Any argus task session — not just a Hera coordinator — accumulates context the longer it runs, with no way to reset the window without abandoning the task's worktree, branch, and in-progress work. This capability gives any task a `task_recycle` primitive: kill the current session and start a fresh, empty-context one on the identical task row, seeded with a caller-supplied handoff note plus the original prompt as background. It is the plain-task sibling of `recycle_coord` (see `coordinator-context-management`), sharing its kill/restart mechanics but requiring no Hera role or binding.
## Requirements
### Requirement: task_recycle resets a task's context window without losing its place

The system SHALL provide a daemon-side primitive that terminates a task's running agent session and starts a fresh one on the identical argus task row — same worktree, same branch, same task ID — seeded with a caller-supplied handoff note plus the task's original prompt as historical background. Unlike `recycle_coord`, this primitive SHALL require no Hera role or binding: it operates directly on the task.

The primitive SHALL defer the actual kill-and-restart until the task's live session reaches genuine idleness (no forced interruption mid-turn), since the call is normally made BY the session it will kill from inside its own tool-call turn. A missing or already-exited session SHALL count as idle immediately. If the session never goes idle within a bounded timeout, the primitive SHALL give up without restarting the task and SHALL leave the task's `SessionID`/`Prompt` unchanged.

Before restarting, the primitive SHALL terminate any stray background job tied to the outgoing session, the same guard `recycle_coord` applies.

The restarted session SHALL be launched with a freshly minted `SessionID` when the task's backend family pins its ID at start (persisted on the task row and passed to the spawn as `--session-id`), so the live session is resumable without waiting for exit capture. Capture-style families SHALL have the ID cleared to empty.

#### Scenario: Restart replaces the stale session ID and seeds the fresh prompt

- **WHEN** the primitive restarts a task on a pin-style backend with an outgoing `SessionID` and a caller-supplied handoff note
- **THEN** the persisted task row has a non-empty `SessionID` different from the outgoing one, the spawn command includes `--session-id` with that value, and a `Prompt` containing both the handoff note and the original prompt, with the handoff note appearing before the original prompt

#### Scenario: No live session restarts immediately

- **WHEN** the primitive is invoked for a task with no live session (already exited, or never started)
- **THEN** it starts a fresh session directly rather than waiting or erroring

#### Scenario: A session that never goes idle does not restart

- **WHEN** the task's live session never reaches idleness before the bounded timeout elapses
- **THEN** the primitive gives up without killing the session or mutating the task row

### Requirement: Seed prompt orders the handoff note before the original prompt

The system SHALL compose the fresh session's opening prompt with the handoff note FIRST, followed by the task's original prompt explicitly marked as historical background not to be treated as a current instruction — mirroring `hera.BuildRecycleSeedPrompt`'s rationale that a fresh session should anchor on current state rather than re-deriving "start from scratch" from a stale mission that may already be substantially done.

#### Scenario: Handoff note precedes the original prompt

- **WHEN** the seed prompt is built from an original prompt and a handoff note
- **THEN** the handoff note's text appears earlier in the composed prompt than the original prompt's text

### Requirement: Task-recycle seed carries the last exchange and is built at restart time
The `task_recycle` seed prompt SHALL be composed after the idle wait, at restart time, and SHALL include the daemon-extracted "Last exchange with the user before the reset" section and the relay directive, with the same fail-open, size-capped, local-only rules as the hera recycle seed. The `task_recycle` tool and skill SHALL require the handoff_note to include an "Open with the user" section.

#### Scenario: Final reply is captured
- **WHEN** the session writes its final reply to the user after calling task_recycle and then goes idle
- **THEN** the seed built at restart includes that reply in the last-exchange section

### Requirement: Resident recycle seed re-reads the vault

When the recycled task is the resident, the seed prompt SHALL order: the caller-supplied handoff note first, then the last-exchange section and relay directive required by "Task-recycle seed carries the last exchange and is built at restart time", then an instruction to read `resident/open-loops.md`, then `resident/profile.md`, then continue, then the resident's persistent orientation as background. The seed SHALL need no further tool calls beyond those reads. Non-resident tasks SHALL keep the handoff, last-exchange, relay-directive, then original-prompt seed.

#### Scenario: Handoff, then open loops

- **WHEN** the resident is recycled with a handoff note
- **THEN** the composed seed places the handoff note before the instruction to read `resident/open-loops.md`, which precedes the orientation

#### Scenario: Resident seed relays the last exchange

- **WHEN** the resident is recycled after replying to the user
- **THEN** the last-exchange section and relay directive appear after the handoff note and before the open-loops instruction

#### Scenario: Plain tasks unchanged

- **WHEN** a non-resident task is recycled
- **THEN** its seed is the handoff note, the last-exchange section and relay directive, followed by the original prompt

### Requirement: Only the resident recycles the resident

The `task_recycle` MCP tool SHALL bind a connection that carries a caller identity (the resident's spawn URL `?caller=`) to recycling only that caller's own task, and SHALL refuse to recycle the resident unless the caller is the resident itself, failing closed when the caller is absent. Any other session's `handoff_note` would otherwise become the fresh resident's own authoritative handoff. Unbound callers SHALL keep recycling ordinary tasks unchanged.

#### Scenario: Unbound caller targets the resident

- **WHEN** a connection with no caller identity calls `task_recycle` for the resident, by id or by its working directory
- **THEN** the call is refused and no recycle is scheduled

#### Scenario: Another task targets the resident

- **WHEN** a connection bound to a non-resident task calls `task_recycle` for the resident
- **THEN** the call is refused and no recycle is scheduled

#### Scenario: Resident recycles itself

- **WHEN** the resident's own connection calls `task_recycle` for itself
- **THEN** the recycle is scheduled

### Requirement: One recycle runner for the resident

For a task where `IsResident()`, every recycle trigger SHALL execute through the resident recycle runner: the agent's `task_recycle` call, a Hera `request_recycle` flag acted on by the recycle watcher, a human-forced coordinator recycle and the TUI bounce action. The Hera coordinator runner's restart step SHALL delegate to the same restart path (stray-job cleanup, resident PTY role, resident seed), and the in-flight guard SHALL be shared so two triggers cannot overlap or double-restart. The resident seed SHALL keep its ordering (handoff note, open-loops-first) and SHALL gain a team section listing each worker's role name, project, status, last self-reported status line, unread messages and whether it is flagged blocked. Recycling SHALL NOT end the resident's binding or touch any worker.

#### Scenario: Two triggers, one recycle

- **WHEN** the resident calls `task_recycle` and the watcher also fires for the same resident
- **THEN** exactly one restart occurs and the second trigger is rejected or no-ops

#### Scenario: Hera trigger uses the resident seed

- **WHEN** a human forces a coordinator recycle on the resident
- **THEN** the fresh session is seeded with the resident seed including the team section, not the coordinator plan-state seed

#### Scenario: Team section content

- **WHEN** the resident has a worker flagged blocked and recycles
- **THEN** the seed names that worker and marks it blocked

#### Scenario: Workers survive

- **WHEN** a recycle completes
- **THEN** every worker session and binding is unchanged
