## MODIFIED Requirements

### Requirement: Live workspace events and connection recovery

The workspace SHALL subscribe to existing daemon events before reading its task baseline, buffer during snapshots and apply buffered events without notification replay. Named task/session events SHALL update the rail immediately without waiting for a task poll. Archive envelopes SHALL request authoritative reconciliation. Every archive envelope SHALL immediately mark its task archive pending and reject composer and raw terminal input in primary and split views, without changing the row's filter membership or position. The row SHALL show a subtle Archive pending indication. Only an authoritative task read started after that envelope SHALL clear pending and determine archived state; failed reads SHALL retain pending and retry on the normal reconciliation schedule. The client SHALL NOT infer a live/replay boundary from snapshot completion. A low-frequency reconciliation snapshot SHALL retain the native client fallback for REST mutations that emit no event. Unknown events and resync SHALL request a fresh snapshot. Stream failure SHALL show a persistent Retry connection banner, retry with a bounded delay and preserve current task and drafts. Footer status SHALL remain a plain label without retry instructions and the separate rail refresh control SHALL be removed. Terminal Reconnect SHALL retain its session-scoped purpose. Disconnect SHALL abort the stream and all retry work.

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
