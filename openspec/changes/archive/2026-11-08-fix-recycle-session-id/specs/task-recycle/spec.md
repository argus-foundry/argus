## MODIFIED Requirements

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

