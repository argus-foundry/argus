## ADDED Requirements

### Requirement: Task mutations invalidate the rail through SSE. Replace full raw pin writes with targeted POST /pin.

Task invalidation events coalesce authoritative snapshots. Subscribe before snapshots, retain buffered events across failed reads, and resync on reconnect, unknown envelopes and visibility return. Keep the 30 second visible-document task poll until the first task.updated event, then stop it. Reconnect resets event-support observation and restores the fallback until another task.updated. Retry connection remains a recovery path for persisted-event failures. Pin posts only pinned and applies returned pinned/archived flags, including restoration. Task snapshots SHALL reconcile present boolean pinned fields on initial load, reconnect, resync and fallback polls, retaining cached pins when older daemons omit the field. Snapshots started before a newer local or SSE pin observation SHALL NOT regress that observation. Pin invalidations SHALL read the single-task DTO behind connection and per-task cancellation fences, without extra raw, selection or menu reads.

#### Scenario: Obsolete responses

- **WHEN** a task or connection changes while a request is pending
- **THEN** late responses cannot overwrite the current view or draft

#### Scenario: Unavailable API

- **WHEN** a read or mutation fails
- **THEN** the view shows actionable retry feedback without claiming success

## MODIFIED Requirements

### Requirement: Task rail quick actions

The workspace SHALL expose task settings, explicit Pin/Unpin and copies through task row menus, reachable by context click and keyboard. Workflow status SHALL be edited only through the task settings select. Ordinary task Pin SHALL consume supplied boolean task state without another selection raw read, retain cached state when an older daemon omits pinned, POST only the desired pinned flag to the targeted pin endpoint and apply returned pinned/archived state, including restoration. Coordinator Pin/Unpin SHALL instead use the orchestrator's pin state and targeted authenticated endpoints without full task-record writes. All live Hera-bound tasks, including freelance bindings, SHALL remain available without a separate visibility filter.

#### Scenario: Captured row action

- **WHEN** a row action targets a task other than the current main task
- **THEN** only the captured row's task is changed
- **AND** failure leaves the main task and draft available

#### Scenario: Existing task-menu keyboard entry

- **WHEN** ArrowDown opens a task's actions menu
- **THEN** Task settings retains initial focus before the additional quick actions

### Requirement: Live workspace events and connection recovery

The workspace SHALL subscribe to existing daemon events before reading its task baseline, buffer during snapshots and apply buffered events without notification replay. Named task/session events SHALL update the rail immediately without waiting for a task poll. Archive envelopes SHALL request authoritative reconciliation. Every archive envelope SHALL immediately mark its task archive pending and reject composer and raw terminal input in primary and split views, without changing the row's filter membership or position. The row SHALL show a subtle Archive pending indication. Only an authoritative task read started after that envelope SHALL clear pending and determine archived state; failed reads SHALL retain pending until reconnect, visibility return, Retry connection or another invalidation triggers a successful snapshot. The client SHALL NOT infer a live/replay boundary from snapshot completion. Task mutation invalidations SHALL coalesce authoritative snapshots with a 30 second visible-document fallback until the first task.updated event on the current stream. Reconnect SHALL reset that observation and restore the fallback until task.updated is seen again. Reconnect, visibility return and Retry connection SHALL recover missing persisted events. Unknown events and resync SHALL request a fresh snapshot. Stream failure SHALL show a persistent Retry connection banner, retry with a bounded delay and preserve current task and drafts. Footer status SHALL remain a plain label without retry instructions and the separate rail refresh control SHALL be removed. Terminal Reconnect SHALL retain its session-scoped purpose. Disconnect SHALL abort the stream and all retry work.

#### Scenario: Snapshot fence and retry

- **WHEN** a named event arrives while a task baseline is loading
- **THEN** the event is applied after that baseline
- **AND** reconnect uses the latest event cursor and refreshes the baseline before live updates resume

#### Scenario: Archive history after restore

- **WHEN** initial connection or reconnect replays an archive event for a task subsequently restored without an inverse event
- **THEN** authoritative task reads SHALL determine archive state while other buffered named events retain the snapshot fence
- **AND** delayed history arriving after a baseline SHALL leave the restored task in Active with input blocked until a later authoritative read returns archived=false, clears pending and restores input according to its session state

#### Scenario: Archive pending fails safe during reconciliation

- **WHEN** any archive envelope arrives, including during reconnect or while an ordinary task snapshot is held
- **THEN** the task SHALL retain its current rail position with an Archive pending indication and reject composer and raw terminal input in primary and split views
- **AND** one input gate SHALL check authoritative workspace task state immediately before every input POST, including keys, paste, composer and attachment submission
- **AND** held or failed task reads, terminal recovery and cached resume SHALL retain those restrictions; terminal DTO merges and reads started before the envelope SHALL NOT clear pending
- **AND** a later authoritative read SHALL move archived=true tasks to Archive, or clear pending and restore eligible input for archived=false tasks

#### Scenario: Backend archive input guard follow-up

- **WHEN** a pure event-stream outage occurs without an archive envelope
- **THEN** this frontend SHALL NOT claim an input fence
- **AND** named backend follow-up `reject-archived-task-input` SHALL make daemon handleWriteInput reject archived tasks; no backend change is included here

### Requirement: Visible document polling
Periodic context and fallback task snapshot reads SHALL pause while the optional workspace document is hidden and SHALL refresh once on becoming visible. Context and session usage SHALL refresh on the foreground 15 second cadence. The 30 second task snapshot fallback SHALL run only until task.updated is observed on the current stream and SHALL restart on reconnect. SSE SHALL remain connected. Hidden usage events SHALL invalidate obsolete session state without starting reads, and visibility return SHALL reconcile once.

#### Scenario: Hidden document returns
- **WHEN** a connected workspace remains hidden for sixty seconds
- **THEN** its context and fallback timer polls SHALL make no API reads
- **AND** returning to visibility SHALL refresh each poll source once without reconnecting SSE
