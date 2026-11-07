## MODIFIED Requirements

### Requirement: Rail configuration and task settings
Project Configuration gears SHALL open the existing Settings project editor directly and SHALL be hidden when no configuration action is available. Repository navigation SHALL own the only project list and Add repository entry; the Projects overview SHALL be removed. Task rows SHALL offer a hover/focus actions button left of the existing archive icon, without selecting the row. Its menu SHALL offer Task settings, explicit Pin/Unpin and copying available task ID, branch and worktree values, followed by a separated red Delete action in Active and Archived. Archive/restore SHALL remain on the rail, in task settings and on the shortcut; the header and menu archive actions SHALL be removed. Menus SHALL support keyboard dismissal, focus restoration and periodic rail refresh without losing their target.

#### Scenario: Inspect and change a captured task
- **WHEN** Task settings is chosen for a task
- **THEN** a main-pane task settings view SHALL load that captured task's DTO/raw facts and existing optional account/backend/PR metadata
- **AND** rename and workflow status SHALL use dedicated endpoints with inline failure recovery
- **AND** supported session/account/fork/delete actions SHALL use existing endpoints, with explicit confirmation for stopping, resuming, moving account or deleting
- **AND** account movement SHALL appear only for a verified Claude backend with compatible choices and SHALL never silently force a busy agent
- **AND** failed changes SHALL keep entered values; closed, disconnected or superseded views SHALL reject late results
- **AND** a newer retry SHALL cancel and supersede older reads of the same task without overwriting newer edits
- **AND** a failed details read after a confirmed mutation SHALL remain visible with recovery rather than being replaced by a successful-load message
- **AND** readonly facts SHALL NOT become generic raw-task overwrites or claims of runtime model/sandbox/branch changes
- **AND** Escape, Back and Forward SHALL restore the prior workspace; selecting another task SHALL leave settings without dispatching a pending action to the new task

### Requirement: Shared workspace operations

Header, task settings and Open PR SHALL share credential-free HTTP(S) PR resolution, preferring pr.url metadata and falling back to output links marked isPR. Output links SHALL share one read within a task selection. Backend and full-project rosters SHALL share cached reads with independent consumer cancellation, clone isolation, failed-read eviction and invalidation on settings writes or credential changes. Explicit project refresh SHALL bypass old roster results.

All confirmations SHALL use one shared modal with optional structured task details and a danger treatment supporting Cancel, Escape, focus restoration, owning-section cancellation and async mutation errors. Clipboard operations SHALL share success/failure feedback while retaining credential-specific warnings. Creation and move SHALL share explicit account policies; moving SHALL remain Claude-only with a visible explanation. Task settings SHALL expose only one Resume agent start action using the resume endpoint, which also guards pending daemon rerender restarts. Copy worktree path SHALL be named explicitly in shortcut help.

#### Scenario: Canceled consumer does not cancel a shared roster
- **WHEN** one of two roster consumers is destroyed
- **THEN** the remaining consumer SHALL still receive the response
- **AND** a failed stale read SHALL NOT evict its replacement

#### Scenario: Cancel a confirmation
- **WHEN** a user cancels or its owning section is destroyed
- **THEN** the captured action SHALL issue no new mutation
- **AND** focus SHALL return to the originating control if it still exists

#### Scenario: Project roster refresh after cached success
- **WHEN** an explicit or periodic task snapshot follows a completed project-roster read
- **THEN** the shared reader SHALL revalidate the roster rather than replay the settled result
- **AND** concurrent readers SHALL still share an in-flight request
- **AND** a failed revalidation SHALL preserve the last rendered projects with the existing warning

## ADDED Requirements

### Requirement: Rail task deletion review
Every task row SHALL offer Delete as its separated final red menu entry. The shared modal SHALL default focus to Cancel, require no typing and show task name, ID, project, branch and worktree. It SHALL explain record, messages/metadata, session logs/artifacts, forced worktree removal, attempted local/remote branch deletion, stopped session and ended live Hera bindings. Cleanup SHALL be described as background work whose completion cannot be verified here. Cheap existing REST observations of live sessions, Hera bindings, reported changed files and PR state/link SHALL be advisory and never block deletion.

#### Scenario: Confirm deletion
- **WHEN** Delete is confirmed
- **THEN** the captured task ID SHALL receive exactly one DELETE request
- **AND** success SHALL remove its row before refresh, detach a deleted open viewer and select a sensible neighbor without discarding other drafts
- **AND** focus SHALL reach the next remaining task row or the current filter when empty

#### Scenario: Cancel or fail
- **WHEN** the review is canceled or its request fails
- **THEN** cancellation SHALL send no mutation and failure SHALL retain the row with an alert
- **AND** no delete SHALL be retried automatically
