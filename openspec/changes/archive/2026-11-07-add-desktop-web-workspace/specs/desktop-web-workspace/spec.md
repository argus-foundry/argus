## ADDED Requirements

### Requirement: Standalone additive workspace
The workspace SHALL run as a separate loopback frontend using existing authenticated REST and SSE operations through its development proxy. It SHALL NOT require Go, daemon, existing static shell, REST contract, schema or backend behavior changes. The existing web UI SHALL remain available and unchanged.

#### Scenario: Run alongside the existing UI
- **WHEN** the standalone client is started
- **THEN** it SHALL be served on its own loopback port and forward existing authenticated `/api/` operations with proxy security filtering to one configured loopback daemon
- **AND** response headers including `X-Output-Total` SHALL remain available to the client
- **AND** the existing web UI and daemon remain untouched

#### Scenario: Preserve application identity
- **WHEN** the optional workspace loads
- **THEN** the tab title SHALL be Argus and the existing favicon and Apple touch icon SHALL be served read-only without copying or changing the original assets

### Requirement: Task workspace and navigation
The client SHALL present configured projects and their tasks in a persistent navigation rail, with a central agent or review area and a tools rail. Task and review selection SHALL not create or alter tasks, worktrees, branches or agent processes, except for the guarded existing PTY resize operation defined below. Settings and task creation SHALL be integrated; unsupported Hera mutations remain excluded.

#### Scenario: Switch task or open review
- **WHEN** a user selects a task or changed file
- **THEN** the client SHALL display that task or its review without sending agent input or changing lifecycle state
- **AND** returning to a task SHALL retain its in-memory draft

#### Scenario: Configured project has no tasks
- **WHEN** a configured project has no tasks in the selected active or archive filter
- **THEN** its navigation group SHALL remain visible as a project header without counts, disclosure or expanded children
- **AND** a search query SHALL hide groups without matching tasks

#### Scenario: Project listing fails independently of tasks
- **WHEN** tasks load successfully but the configured project list fails without an authentication error
- **THEN** the client SHALL keep the task connection and apply the task result
- **AND** initial connection SHALL fall back to task-derived groups while refresh retains the current connection's last project list
- **AND** a project-specific warning SHALL offer refresh, clear after a successful project read or disconnect, and respect request cancellation and generation guards
- **AND** project authentication failures SHALL remain connection errors rather than falling back silently

### Requirement: Adjustable workspace layout
Navigation and tools rails SHALL collapse independently. Agent focus SHALL hide both rails and restore their previous visibility. Panel resizing SHALL be available with usable bounds. Geometry preferences may be stored locally. Drafts SHALL remain memory-only; credentials follow Remembered token below.

#### Scenario: Resize the workspace
- **WHEN** the user changes panel layout or browser viewport
- **THEN** the layout SHALL adapt and eligible visible idle terminals SHALL fit after settled geometry under the guarded automatic fitting requirement
- **AND** Fit session SHALL be the explicit action that requests the selected session's current visible size

#### Scenario: Contain panels through layout changes
- **WHEN** rails are toggled, resized or replaced by compact drawers, including with long paths and branch names
- **THEN** each remaining panel SHALL retain its intended column and fit within the viewport without page-level horizontal scrolling
- **AND** hiding navigation or entering agent focus SHALL expand the terminal column rather than place it in a collapsed rail track
- **AND** directory prefixes and branch names SHALL ellipsize within their panel; filenames SHALL remain intact and scroll locally when necessary while wide terminal or diff content scrolls only within its own viewer

### Requirement: Existing agent and review tools
The agent view SHALL use the real raw agent terminal. Tools SHALL use existing APIs for Changes with flat/tree presentations, registered Artifacts and task Info. Review SHALL support unified and split views from returned diff text. It SHALL NOT imply conversation rendering, independent shell sessions, complete repository listing or generic source reads.

#### Scenario: Inspect available work
- **WHEN** a user selects an available changed or untracked path
- **THEN** the client SHALL display its returned diff in unified or split form
- **AND** registered artifact previews SHALL retain safe preview boundaries

#### Scenario: Bound large diff rendering
- **WHEN** returned diff text contains more than 6,000 parsed lines
- **THEN** unified and split review SHALL render only the first 6,000 parsed lines
- **AND** both layouts SHALL disclose the truncation and direct the user to the original UI or a local editor for the full diff

#### Scenario: Expand a directory with quoted filenames
- **WHEN** an existing directory response contains a git-quoted filename
- **THEN** the client SHALL decode that path before displaying it or requesting its diff

#### Scenario: Fit the terminal
- **WHEN** a user explicitly selects Fit session
- **THEN** the client SHALL request resize for the selected live PTY
- **AND** automatic layout-driven fitting SHALL follow the settled/visible/idle rules below, while explicit Fit remains a manual fallback

### Requirement: Safe client state and existing lifecycle
Task drafts SHALL remain in browser memory, scoped to their task. Browser selection guards SHALL prevent late UI responses from being shown against the wrong selected task, without claiming stronger backend input race or error guarantees. Existing archive and restore operations SHALL retain their current consequences and require clear explanation and confirmation.

#### Scenario: Archive or restore
- **WHEN** a user invokes an existing archive or restore operation
- **THEN** the client SHALL explain the known effects before confirmation
- **AND** it SHALL NOT promise backend behavior beyond the existing operation

#### Scenario: Protect local data
- **WHEN** the client handles credentials, drafts, API text or artifact previews
- **THEN** drafts SHALL not be persisted and tokens and drafts SHALL not be logged; token persistence SHALL follow Remembered token
- **AND** API text SHALL render safely and HTML artifact previews SHALL be sandboxed with scripts disabled

### Requirement: Terminal continuity without backend changes
The client SHALL render authoritative existing session geometry, preserve raw output bytes and use the existing snapshot byte cursor to attach the SSE stream. Review and panel hiding SHALL NOT stop the stream or process. Switching tasks SHALL release the old viewer and guard callbacks by selection generation. Reconnection SHALL be bounded and provide an explicit retry control. These protections SHALL NOT claim a server enforced process generation guarantee.

#### Scenario: Temporary stream loss
- **WHEN** the selected task's stream disconnects
- **THEN** the browser SHALL retry from its accepted output cursor without appending the snapshot twice
- **AND** retry exhaustion SHALL show an actionable disconnected state without an automatic process restart

#### Scenario: Task refresh supersedes terminal recovery
- **WHEN** a same-task offline/live refresh reconnects during rerender recovery
- **THEN** the superseded recovery timer and ownership SHALL be cleared
- **AND** stale recovery completion SHALL NOT alter the current recovery state
- **AND** a subsequent rerender SHALL still recover and reopen the terminal stream

#### Scenario: Terminal input and a task switch
- **WHEN** a send or keyboard input is queued while selection changes
- **THEN** client input SHALL remain targeted to the captured task or be canceled, never redirected to the newly selected task
- **AND** failed or partially canceled compose SHALL retain its draft and explain that submission is unconfirmed
- **AND** Ctrl+Z SHALL remain blocked to avoid detaching the agent

### Requirement: Accessible compact navigation
Panel controls, tabs, task actions and resize separators SHALL have accessible names, visible focus and selected or expanded state. Separators SHALL support keyboard sizing. At compact widths, panels SHALL become dismissible drawers rather than compressing the terminal and controls beyond use. Tokens and drafts SHALL be discarded on disconnect, with browser viewers cleaned up.

#### Scenario: Close a compact drawer
- **WHEN** a user closes a drawer by its control, backdrop or Escape
- **THEN** keyboard focus SHALL return to its header toggle and background controls SHALL be reachable again
- **AND** both header toggles SHALL remain available in agent focus mode

#### Scenario: Disconnect from Argus
- **WHEN** the user disconnects
- **THEN** active fetches, streams, reconnect timers and viewers SHALL be canceled
- **AND** credentials and task drafts SHALL be cleared without mutating the daemon

### Requirement: Honest repository state
The client SHALL distinguish request failures from successful results but SHALL NOT invent error classification or comparison metadata that the current API lacks. Changed-file reads and artifact previews SHALL reject late results for another selected task. Missing tasks and removed artifact bytes SHALL offer refresh or recovery rather than an endless loading state.

#### Scenario: Existing git response is empty
- **WHEN** an existing git read succeeds with empty fields
- **THEN** the UI SHALL describe that no changes were returned without claiming verified repository health
- **AND** it SHALL NOT imply a complete repository tree or a stable comparison base

### Requirement: Dictation friendly composer
The composer SHALL use a native multiline textarea and receive focus after explicit task selection and successful submission when doing so does not interrupt another control. Enter SHALL send, Shift+Enter SHALL insert a newline, and composition or repeated Enter events SHALL NOT submit. Clipboard text, including multiline dictation, SHALL be inserted without automatic submission. Paste outside editable controls SHALL target the selected task's composer unless a dialog, drawer or terminal owns the interaction.

#### Scenario: Dictate after selecting a task
- **WHEN** a user selects a task on desktop or from a compact drawer
- **THEN** its composer SHALL receive focus after the drawer closes and accept normal browser clipboard paste
- **AND** task refreshes or stream updates SHALL NOT steal focus from search, review or terminal controls

#### Scenario: Submit or add a line
- **WHEN** the user presses ordinary Enter in the composer
- **THEN** one eligible message SHALL be submitted to the selected task
- **AND** Shift+Enter SHALL instead insert a newline without submitting
- **AND** Enter during IME composition, legacy key code 229 or key repeat SHALL NOT send

#### Scenario: Paste dictation without submitting
- **WHEN** dictation pastes text with embedded or trailing newlines
- **THEN** the complete text SHALL remain in that task's draft until explicit submission
- **AND** paste into search, credentials or the active terminal SHALL keep its original destination

#### Scenario: Repeated editing keeps the native field stable
- **WHEN** text is inserted, erased and inserted again or unchanged task state is rendered
- **THEN** the same native textarea SHALL remain editable and focused when already selected
- **AND** unchanged labels and live status SHALL NOT be rebuilt on every render

#### Scenario: Repeated native paste inserts at the selection
- **WHEN** clipboard text reaches the focused composer repeatedly
- **THEN** the composer SHALL insert each payload once at the caret or replace the selected range and synchronize that task's draft
- **AND** browser editing history SHALL support undo and redo when the browser editing command is available, with range replacement as a compatibility fallback
- **AND** empty clipboard text and paste into read-only controls SHALL NOT change the draft

### Requirement: Local dictation isolation
A standalone dictation check SHALL offer the same native text field with xterm absent or mounted, without requiring authentication, connecting to a daemon, sending input or accessing the OS clipboard. Optional diagnostics SHALL contain event and field state metadata only, never dictated text or credentials.

#### Scenario: Compare native text insertion
- **WHEN** the user opens the dictation check in either mode
- **THEN** no API or agent request SHALL be made
- **AND** any recorded diagnostics SHALL remain in memory and omit text contents

### Requirement: Composer attachments through the existing upload API
The client SHALL accept file drops on the composer and selected task's agent panel, and image paste into the composer, using existing multipart task uploads. File drags anywhere in the window SHALL NOT navigate or open a tab; non-file drags SHALL retain native behavior. Uploads and attachment chips SHALL belong to the task selected when accepted, remain in memory only, and show filenames, image previews, pending/error state, removal and explicit retry. No daemon or original static asset changes are included.

#### Scenario: Drop or paste an attachment
- **WHEN** files are dropped on an eligible task's composer or agent panel, or an image is pasted into the composer
- **THEN** the client SHALL upload with multipart `files` parts without setting Content-Type, with a timeout below the existing 300 second read limit
- **AND** a file drag SHALL highlight its target, with a clear disabled message when no task can take input
- **AND** plain-text paste SHALL retain the accepted explicit insertion behavior without submitting pasted newlines

#### Scenario: Switch task or send attachments
- **WHEN** task selection changes during an upload
- **THEN** its resulting paths and chips SHALL remain associated with the original task
- **AND** explicit send SHALL include ready attachment paths separated by spaces, without an upload-triggered submission
- **AND** removed chips SHALL NOT contribute paths to subsequent sends
- **AND** uploads or errors SHALL require completion, removal or explicit retry before sending

#### Scenario: Remove or abandon an upload
- **WHEN** a chip is removed, successfully sent, or the client disconnects
- **THEN** its local image URL SHALL be revoked and obsolete upload callbacks SHALL be ignored
- **AND** canceled or timed-out uploads SHALL be described as potentially completed on the server
- **AND** removing a chip SHALL NOT imply deletion of an already uploaded file

### Requirement: Rail archive controls
Every task row SHALL offer a small outlined archive icon button, right-aligned and vertically centered, visible on hover and keyboard focus, reachable by Tab and named `Archive <task name>`. Hover SHALL show a subtle rounded row highlight without changing row layout. Archived rows SHALL instead offer a restore icon button named `Restore <task name>`. These buttons SHALL invoke the existing confirmation and API effect without selecting their task.

#### Scenario: Confirm or cancel a rail operation
- **WHEN** a rail archive button is activated
- **THEN** the existing dialog SHALL say `You're about to archive <task name>.` with the name rendered as text and retain the existing consequence explanation
- **AND** confirmation SHALL perform the existing archive request while Cancel and Escape SHALL leave the task unchanged
- **AND** restore SHALL use the existing restore confirmation and operation with the target task name

#### Scenario: Archive the selected task
- **WHEN** the selected task is archived
- **THEN** the same task SHALL remain selected in the main pane while its row is excluded from the current filter
- **AND** its current terminal viewer SHALL remain attached, retain archived input restrictions and offer Restore through the rail, task settings or shortcut

### Requirement: Remember credentials on this device
The loopback workspace SHALL offer Remember on this device, checked by default. Only a successful remembered connection SHALL store the API token in localStorage under `argus-workspace-token`, separate from the original UI's origin and key. This matches the original UI's localStorage security posture. All storage access SHALL be guarded against exceptions and fall back to memory-only operation. Drafts SHALL remain memory-only. Tokens SHALL never be logged or placed in URLs except through the existing EventSource streamUrl mechanism or explicitly generated credential-bearing share links.

#### Scenario: Refresh with a remembered token
- **WHEN** a saved workspace token exists at page load
- **THEN** the workspace SHALL attempt connection automatically
- **AND** pagehide SHALL dispose viewers without deleting the saved token

#### Scenario: Reject a saved token
- **WHEN** automatic connection receives 401 or 403
- **THEN** the saved token SHALL be removed and the login form SHALL show Saved token was rejected
- **AND** failed attempts SHALL never save their candidate token

#### Scenario: Temporary connection failure
- **WHEN** automatic connection fails through a network or 5xx error
- **THEN** the saved token SHALL remain available
- **AND** the login form SHALL show the existing connection error and offer retry with that token

#### Scenario: Decline remembering or disconnect
- **WHEN** a user connects with Remember unchecked
- **THEN** the token SHALL stay only in memory and refresh SHALL require login
- **AND** explicit disconnect SHALL remove the saved token and clear in-memory credentials and drafts

### Requirement: Nested workspace task rail
The optional workspace SHALL derive task parents from existing GET /api/hera live role bindings and orchestrator bridges, without backend changes. A worker SHALL appear below its bound coordinator, recursively for sub-coordinators. Each task SHALL appear once; missing parents, ambiguous bindings and malformed cycles SHALL leave tasks reachable. Root tasks SHALL retain project grouping; descendants in another project SHALL follow their parent. This frontend-only change leaves the original web UI, TUI and macOS behavior unchanged.

#### Scenario: Collapse a task family
- **WHEN** a parent disclosure is activated by pointer, Enter or Space
- **THEN** all its descendants SHALL hide or expand using the project group's native disclosure affordance
- **AND** each disclosure SHALL retain its own state through refresh and filter changes in tab memory, resetting on reload or disconnect like project groups
- **AND** selection and archive buttons SHALL remain separate accessible actions
- **AND** a collapsed parent's row SHALL signal when any non-archived descendant needs input, including blocked Hera roles

#### Scenario: Filter and recover task families
- **WHEN** archive or search filters exclude a parent
- **THEN** matching children SHALL remain reachable as roots and archived tasks SHALL only appear in Archive
- **AND** search SHALL reveal matches regardless of collapsed ancestors without clearing disclosure preferences
- **AND** selecting a task through history SHALL expand its ancestors
- **AND** failed roster reads SHALL leave task navigation usable with a nesting warning, while auth and abort failures retain existing guards

### Requirement: Compact task rows
Task rows SHALL display a single compact line with a status icon, task name and the existing centered hover archive control, retaining nesting and disclosure behavior. Names SHALL truncate with an ellipsis and expose truncated full names through shared data-tooltip help without native title. Rows SHALL omit elapsed time and separate visible status text. Each inline SVG status icon SHALL carry an accessible status label.

#### Scenario: Represent available task states
- **WHEN** a task row renders
- **THEN** active non-idle sessions SHALL show a small animated spinner, and idle/pending/offline tasks without a PR SHALL show a gray branch icon
- **AND** open/review PRs SHALL show a green pull-request icon, approved PRs a distinguishable green variant, complete tasks a purple merged icon, and needs-input a prominent attention icon
- **AND** failed bound Hera roles SHALL show red failure icons
- **AND** reduced-motion preference SHALL stop spinner animation
- **AND** the combined REST `merged-closed` state SHALL use a purple terminal PR icon labeled PR merged or closed without claiming it can distinguish the two
- **AND** task failures outside the Hera roster remain unavailable because the task REST payload has no failure field

#### Scenario: Busy Hera worker already in review
- **WHEN** a session is live and not idle, including a task in review or complete workflow status
- **THEN** the working spinner SHALL represent its activity regardless of workflow status, retaining archived, needs-input, failure and combined terminal PR precedence
- **AND** an idle task in review without cached PR state SHALL remain gray and labeled In review, since workflow status alone does not establish a PR

### Requirement: Conductor composer layout
The optional workspace SHALL show a large rounded draft box with no placeholder text above a bottom bar. Backend/model identity SHALL be read-only; context, attach and send controls SHALL be aligned on the right. A file picker SHALL reuse the existing upload path and attachment constraints. Attachment instructions SHALL appear only for errors, while chips and existing paste/drop behavior remain.

#### Scenario: Native composer actions
- **WHEN** a task is selected and files are chosen with the attach button
- **THEN** uploads belong to that task and no terminal input is sent until explicit send
- **AND** native paste, dictation, IME and per-task drafts preserve existing behavior

### Requirement: Measured context popover
The context control SHALL open an accessible dismissible popover, show only sourced existing REST values with their correct scope, and indicate unavailable context without fabricating a fill or percent. Unsupported backend fields SHALL be omitted. Switching tasks and disconnecting SHALL cancel reads and reject late results.

#### Scenario: Backend measurements are partial
- **WHEN** REST exposes account usage but no context-window measurement
- **THEN** usage is shown only for the task's matching account and backend
- **AND** the context ring remains unknown rather than displaying quota as context
#### Scenario: Live bridge and recorded stamp availability
- **WHEN** the selected task has a live bridge context with a measured percent or measured tokens/window
- **THEN** the ring displays that context percentage and the popover shows available token/window values
- **WHEN** only Hera metadata is present
- **THEN** the popover labels the values as recorded, shows record time and pairs percent with tokens only for equal timestamps
- **AND** a configured recycle budget and historical role token totals never supply an inferred window

#### Scenario: Shared account across backends
- **WHEN** a Codex task uses an account that also supports Claude
- **THEN** the matching configured account label may appear
- **AND** Claude plan, email and account quotas SHALL NOT be attributed to Codex
- **AND** unsupported Codex fields remain absent as documented in web-workspace/docs/context-data.md

#### Scenario: Historical token totals
- **WHEN** a task has exactly one live bound role with measured REST token totals
- **THEN** available counts appear as role-history totals, distinct from the current context breakdown
- **AND** ambiguous multiple role bindings are not summed or arbitrarily selected

### Requirement: Project rows and task creation
Project headers SHALL show a letter avatar and project name, an always-visible New task (+) button and a hover/focus Configuration gear. Project headers SHALL omit task counts. Empty projects SHALL have no disclosure control, expanded state or empty child area, including when previously expanded projects become empty after refresh or archive filters. Non-empty groups SHALL retain existing pointer, keyboard and in-memory disclosure behavior. The Configuration gear SHALL open the project editor directly; no single-item project menu or duplicate New task action SHALL remain.

#### Scenario: Start a task from a project
- **WHEN** New task is chosen from a project's plus button
- **THEN** the main pane SHALL open a task-creation form with that project preselected, a native prompt field and optional name
- **AND** POST /api/tasks SHALL use existing JSON fields and omit backend and account overrides unless explicitly selected, and omit branch and model overrides so daemon defaults apply
- **AND** successful creation SHALL select the new task and open its agent terminal
- **AND** prompt paste SHALL preserve multiline text without submitting; ordinary Enter SHALL submit and Shift+Enter SHALL insert a newline, retaining composition guards
- **AND** errors SHALL show inline while keeping the prompt and name for correction or retry
- **AND** Escape or Cancel SHALL return to the previous workspace view without sending agent input
- **AND** canceling an in-flight request SHALL ignore late results and explain that creation may have completed
- **AND** creation drafts SHALL stay in tab memory and clear on disconnect; pre-creation attachments remain outside scope, with task attachments available after creation

### Requirement: Settings home and project management
The footer SHALL replace its original UI link with an accessible settings gear beside connection status. Settings SHALL retain the workspace's task draft and terminal attachment, support General/repository section navigation and return through Escape, Back or task selection. Closing or disconnecting SHALL cancel stale settings reads and guard late results.

#### Scenario: Edit General settings
- **WHEN** a user opens General
- **THEN** the client SHALL show existing KB, default backend/share project and HTTP API controls, with the API port read-only
- **AND** explicit Save SHALL send only the owned partial settings fields, preserve form data on failure and show restart-required notices without restarting the daemon

#### Scenario: Manage projects
- **WHEN** a user explicitly adds, edits or removes project configuration
- **THEN** the client SHALL use existing project CRUD endpoints, validate required fields and check duplicate names before create
- **AND** editing SHALL preserve all returned sandbox overrides and clearly disclose the existing profile-reset API limitation
- **AND** removing SHALL require confirmation that existing tasks and worktrees remain
- **AND** successful writes SHALL refresh project choices and the task rail while errors retain editable input

### Requirement: Archive without losing workspace context
Archiving or restoring SHALL retain the current rail filter and scroll position and keep the same selected task and main viewer. A task excluded by the current filter SHALL have no selected rail row, even though its main viewer remains open. Restoring through the rail, task settings or shortcut SHALL reselect the row when it is included in the current filter. Unselected row archive/restore SHALL continue to leave main selection unchanged. Archive confirmation, backend effects and archived-task input restrictions remain unchanged.

#### Scenario: Archive the selected task in Active
- **WHEN** the selected task is archived through its rail row, task settings or shortcut
- **THEN** Active SHALL remain open at the same scroll position, the task row SHALL disappear and no row SHALL be selected
- **AND** the same main task and terminal SHALL remain visible with Restore available in the rail, task settings or shortcut
- **AND** a failed follow-up task refresh SHALL NOT select another task or detach that viewer

#### Scenario: Restore without changing filters
- **WHEN** the currently viewed task is restored through the rail, task settings or shortcut
- **THEN** the main viewer SHALL remain on that task and Archive SHALL be available in the rail, task settings or shortcut
- **AND** Active SHALL show and reselect the restored task while Archived SHALL remain Archived with the restored row absent
- **AND** switching to a filter that contains the current task SHALL reveal its selection without changing the main task

### Requirement: Rail configuration and task settings
Project Configuration gears SHALL open the existing Settings project editor directly and SHALL be hidden when no configuration action is available. Repository navigation SHALL own the only project list and Add repository entry; the Projects overview SHALL be removed. Task rows SHALL offer a hover/focus actions button left of the existing archive icon, without selecting the row. Its menu SHALL offer Task settings, explicit Pin/Unpin and copying available task ID, branch and worktree values. Archive/restore SHALL remain on the rail, in task settings and on the shortcut; the header and menu archive actions SHALL be removed. Menus SHALL support keyboard dismissal, focus restoration and periodic rail refresh without losing their target.

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

### Requirement: Responsive terminal viewport
The optional workspace SHALL retain viewport-contained columns at 320px through ultrawide widths. The client terminal host SHALL occupy the pane content box while preserving authoritative PTY dimensions. Narrower terminal output SHALL NOT cause horizontal scroll; wider PTYs MAY scroll only inside the terminal pane.

#### Scenario: Window or rail geometry changes
- **WHEN** a window, rail or focus change alters the terminal pane
- **THEN** client host dimensions track the new content box
- **AND** a visible interactive idle terminal SHALL automatically fit after settled geometry when cells differ
- **AND** busy, recorded, failed or otherwise deferred fitting SHALL retain the mismatch hint for the manual fallback

#### Scenario: Explicit fitting excludes pane decoration
- **WHEN** Fit session is activated
- **THEN** proposed terminal cells exclude the pane padding and scrollbars
- **AND** the fitted terminal stays within the content box to within a cell
- **AND** its fit hint clears after fitting and is shown again only when automatic fitting is deferred or fails

### Requirement: Automation schedules
The optional workspace SHALL list, create, edit, delete and run saved schedules through existing REST endpoints. It SHALL expose project, backend, model, prompt, enabled state and supported cadence; deletion SHALL confirm that already created tasks are unaffected. Failed edits SHALL remain available and mutation retries SHALL be explicit.

#### Scenario: Editing a saved one-shot
- **WHEN** unrelated schedule fields are edited
- **THEN** an unchanged one-shot time SHALL remain untouched even if it is already in the past
- **AND** changing cadence SHALL use the existing schedule/run_once_at contract

### Requirement: Visible System metrics and bounded logs
The System section SHALL display only available REST measurements and host-sleep risk commands. Metrics SHALL poll only while the section and browser tab are visible; leaving the section SHALL cancel pending reads and timers. Logs SHALL read only UX or daemon tails, render plain text and scroll within their panel.

#### Scenario: Changing log selection during a read
- **WHEN** the log source changes or the section closes before completion
- **THEN** the prior request SHALL be canceled and its late result SHALL NOT replace the selected content
- **AND** log contents SHALL never be interpreted as HTML or written to browser storage
### Requirement: Grouped searchable settings
Settings SHALL provide grouped Personal, Agents & environment, Automation, System and Repositories navigation. Personal SHALL contain General and Notifications & devices; Agents & environment SHALL contain Agents and Sandbox; Automation SHALL contain Schedules; System SHALL contain System and Logs. Each configured repository SHALL have a direct editor entry and an Add repository action. Sections pending implementation SHALL say so without inventing settings or API capabilities.

#### Scenario: Search and open a repository
- **WHEN** a user filters settings navigation by a section label, repository name or path
- **THEN** only matching navigation entries and their groups SHALL remain visible
- **AND** choosing a repository SHALL open its existing project editor with returned overrides preserved
- **AND** the public openProject(name, {history}) entry point SHALL remain available for rail Configuration actions
- **AND** mobile navigation SHALL use a bounded grouped section selector beside Back

#### Scenario: Register settings sections independently
- **WHEN** an additional settings section is registered
- **THEN** it SHALL receive API, host, status, changed, busy and baseline callbacks and SHALL abort reads and release resources on destroy
- **AND** closing, disconnecting or selecting another section SHALL guard late results

### Requirement: Sandbox and agent settings parity
Sandbox SHALL show host availability, global enablement, denied read paths and extra write paths through existing settings APIs. Writes SHALL send only owned sandbox fields, preserve unedited Apple Events grants, report master-token failures and explain that new sessions receive changes. Pending write-path proposals SHALL support editable paths, project/global scope, add/remove, approval and rejection, with confirmation showing paths and scope before granting access.

#### Scenario: Save sandbox paths or decide a proposal
- **WHEN** a user explicitly saves sandbox settings or confirms a proposal decision
- **THEN** the client SHALL use the existing master-gated REST operation without restarting sessions
- **AND** failed writes SHALL retain editable input and show recovery guidance
- **AND** proposal failures SHALL NOT discard draft paths and scope
- **AND** unrelated settings SHALL remain outside the write payload

#### Scenario: Inspect agents and accounts
- **WHEN** a user opens Agents
- **THEN** configured backends and account metadata SHALL be shown read-only using existing endpoints
- **AND** missing or unsupported account sign-in state SHALL be labelled unknown or unsupported, without inventing identity or usage
- **AND** one failed metadata read SHALL NOT hide the other successful list

### Requirement: Notifications and device settings parity
Notifications & devices SHALL support browser push enable/disable, registered-device listing/removal and explicit test notification via existing APIs. Permission requests and push registration SHALL happen only after a user action; unsupported/denied states SHALL be shown honestly. A workspace-local worker SHALL handle push notifications without caching authenticated requests or shell files. API token listing, creation and revocation SHALL use existing master-token endpoints and show a minted credential only in the current section until dismissed or closed.

#### Scenario: Manage notification devices and API tokens
- **WHEN** a user enables browser notifications, sends a test, removes a device or creates/revokes a token
- **THEN** only the corresponding existing API operation SHALL run
- **AND** remote-device removal and token revocation SHALL require explicit confirmation
- **AND** failed requests SHALL retain actionable controls and master-token failures SHALL be explained
- **AND** closing or disconnecting SHALL clear any newly displayed credential

#### Scenario: Build an iOS share shortcut URL
- **WHEN** a user explicitly generates a share URL for a supplied original Argus origin
- **THEN** the client SHALL show the existing original UI /share URL with the current credential and Shortcut Input placeholder
- **AND** it SHALL first explain that the URL contains a credential and offer explicit copy and hide actions
- **AND** generation SHALL send no network request, reject non-HTTPS non-loopback origins, and clear the URL when the section closes

### Requirement: Desktop browser keyboard navigation

The workspace SHALL provide a discoverable shortcuts dialog and browser equivalents for task actions, detail tabs, filter focus, schedules and next attention. Editable controls SHALL retain their ordinary editing keys. Global Alt chords SHALL resolve physical key codes so Option-produced Unicode cannot change their meaning. Terminal focus SHALL claim only Cmd+arrows, Shift+Up/Down/PageUp/PageDown/End Cmd+Shift+C/U and Cmd+Shift+Enter, forwarding all other input unchanged. Scroll and copy actions SHALL use xterm APIs without sending PTY bytes. Destructive shortcuts SHALL use the existing confirmation flow.

#### Scenario: Terminal safety

- **WHEN** the terminal receives a claimed navigation or scroll chord
- **THEN** the client performs the documented action without submitting terminal input
- **AND** ordinary typing, Ctrl keys and clipboard editing retain their native behavior

### Requirement: Maintenance and Argus update settings
System navigation SHALL include Maintenance and Update. Maintenance SHALL require explicit confirmation that cleanup deletes completed task data and sessions/logs, removes their worktrees/branches and sweeps orphaned worktree directories, then show the existing endpoint's counts. Update SHALL expose master-gated source-path GET/PUT and the existing update operation, with explicit confirmation that it rebuilds Argus and requests a daemon restart.

#### Scenario: Clean completed tasks explicitly
- **WHEN** cleanup is confirmed
- **THEN** the client SHALL call POST /api/maintenance/prune-completed once, disable mutation controls while pending and show returned pruned/worktree/orphan/skipped-Hera-bound counts
- **AND** cancellation SHALL send no request, failures SHALL explain uncertainty without automatic retry, and success SHALL refresh task metadata

#### Scenario: Save a source path and update Argus
- **WHEN** an authenticated user saves a source path or confirms an update
- **THEN** the client SHALL use existing GET/PUT /api/source-path and POST /api/update, explain master-token failures, retain failed path edits and show build output as plain text
- **AND** unsaved or empty source paths SHALL block update so the displayed path matches the daemon's saved path
- **AND** a successful requested restart SHALL explain the five-second page reload, while closing the section cancels its reload timer and ignores late results
- **AND** automated verification SHALL stub update and cleanup operations or use only an isolated harness, never the live daemon

### Requirement: Claude session selection

The workspace SHALL offer a captured-task Claude session picker beside Stop/Resume in task settings, using the existing session-switcher capability and REST endpoints. The terminal toolbar SHALL omit its former Switch control while Split/Fit retain their existing owners. It SHALL show titles, activity, branch, PR references and the current session, disable duplicate submission, cancel reads on task change and retain actionable failure information. A confirmed switch SHALL refresh the selected terminal; unchanged SHALL remain a successful no-op.

#### Scenario: Switch a conversation

- **WHEN** a supported task's picker selects another session
- **THEN** POST claude-session sends that captured task and session ID
- **AND** another task cannot receive the switch or late response

### Requirement: Unified project task rail

The workspace SHALL always group tasks by project with coordinator nesting. Compact one-line rows SHALL retain status icons, truncated task names and live Hera role-status tags. No Project/Orchestration control or Hera-managed task filter SHALL appear. The client SHALL ignore stored rail-grouping preferences without migration. The Mac Hera sidebar mode and show-Hera-managed filter are intentional divergences because the nested project rail covers task navigation.

Search, archive, pin, actions and needs-input emphasis with collapsed-parent rollup SHALL remain available. Every task SHALL remain reachable regardless of Hera binding or project. Cross-project workers SHALL follow their visible coordinator, and filtering out a parent SHALL promote matching descendants. Selecting a coordinator SHALL open the normal task view unless split was explicitly enabled. Roster polling SHALL run only when role tags, nesting or split presentation use it, without an orchestration-grouping condition.

Role tags SHALL show blocked or failed first, then working, then ready when the task carries ready_to_close, then the role status. A working role SHALL never show ready from a stale ready_to_close mark, and when a task has several live bindings the rail SHALL pick the binding by the same precedence.

#### Scenario: Ignore obsolete rail preferences

- **WHEN** the workspace loads with a stored orchestration grouping preference
- **THEN** project groups with coordinator nesting SHALL render
- **AND** no grouping control, grouping shortcut or Hera task visibility filter SHALL appear
- **AND** split, role tags, search, archive and pin SHALL remain available

#### Scenario: Resumed worker does not show ready

- **WHEN** a live role reports working while its task still carries ready_to_close
- **THEN** its role tag SHALL show working, not ready
- **AND** a done or idle role with ready_to_close SHALL still show ready

#### Scenario: Failed binding remains visible among competing live bindings

- **WHEN** a task has a failed live binding alongside a working or ready live binding
- **THEN** the rail SHALL show failed regardless of binding order
- **AND** a blocked live binding SHALL take precedence over failed regardless of binding order

#### Scenario: Cross-project worker remains reachable

- **WHEN** a live Hera worker belongs to a different project than its coordinator
- **THEN** the worker SHALL appear once beneath the coordinator in the coordinator's project
- **AND** searching for the worker's project SHALL promote it into its own project when the coordinator does not match

### Requirement: Explicit split terminal view

The agent screen SHALL offer an explicit split toggle for any selected task, independent of rail grouping and coordinator selection. It SHALL default off and persist its enabled state per device through guarded localStorage operations. The left terminal SHALL remain the current task. The right pane SHALL offer a picker for any other task; a coordinator task MAY default to its selected live worker, while ordinary tasks SHALL start with an empty right pane. A compact collapsible coordinator roster SHALL be available in split mode without enabling split automatically.

Closing split SHALL return to the single view without disposing either retained controller. A task controller SHALL occupy only one slot at a time, and input/state callbacks SHALL remain scoped to the current slot. The shortcut help SHALL document Alt+Shift+D globally and Cmd+Shift+Enter in the terminal; the terminal-safe allowlist SHALL claim the latter without sending PTY bytes. No Hera mutation controls SHALL appear.

#### Scenario: Explicit split and retained secondary task

- **WHEN** split is enabled and a secondary task is chosen
- **THEN** the current task stays left and the selected other task appears right
- **AND** closing and reopening split reuses the retained secondary terminal
- **AND** selecting a task never enables split implicitly

### Requirement: Task rail quick actions

The workspace SHALL expose task settings, explicit Pin/Unpin and copies through task row menus, reachable by context click and keyboard. Workflow status SHALL be edited only through the task settings select. Pin SHALL consume supplied boolean task state without another selection raw read, lazily resolve unknown menu state, preserve the fresh raw record when mutating and reject archived tasks. All live Hera-bound tasks, including freelance bindings, SHALL remain available without a separate visibility filter.

#### Scenario: Captured row action

- **WHEN** a row action targets a task other than the current main task
- **THEN** only the captured row's task is changed
- **AND** failure leaves the main task and draft available

#### Scenario: Existing task-menu keyboard entry

- **WHEN** ArrowDown opens a task's actions menu
- **THEN** Task settings retains initial focus before the additional quick actions

### Requirement: Complete task information and files

Changes SHALL render one git-status path list in flat/tree modes, lazily expand untracked directories through the existing files endpoint, open returned diffs and offer relative/absolute path copy. Info SHALL own immutable task facts, created time, original prompt and credential-free HTTP(S) output links. Settings SHALL keep editable controls and session ID/sandbox facts missing from Info. The task name tooltip SHALL identify the full name and project without repeated workflow/elapsed facts. Header branch copy and PR links SHALL remain available across task tabs. Creation SHALL allow an optional backend choice, preserving daemon defaults when omitted. Native Finder access SHALL use path copy in the browser.

#### Scenario: Safe links and paths

- **WHEN** the daemon returns task links or changed file paths
- **THEN** paths remain text and only safe HTTP(S) links become anchors
- **AND** late reads cannot replace another task's information

### Requirement: Live workspace events and connection recovery

The workspace SHALL subscribe to existing daemon events before reading its task baseline, buffer during snapshots and apply buffered events without notification replay. Named task/session events SHALL update the rail immediately without waiting for a task poll. A low-frequency reconciliation snapshot SHALL retain the native client fallback for REST mutations that emit no event. Unknown events and resync SHALL request a fresh snapshot. Stream failure SHALL show a persistent Retry connection banner, retry with a bounded delay and preserve current task and drafts. Footer status SHALL remain a plain label without retry instructions and the separate rail refresh control SHALL be removed. Terminal Reconnect SHALL retain its session-scoped purpose. Disconnect SHALL abort the stream and all retry work.

#### Scenario: Snapshot fence and retry

- **WHEN** a named event arrives while a task baseline is loading
- **THEN** the event is applied after that baseline
- **AND** reconnect uses the latest event cursor and refreshes the baseline before live updates resume

### Requirement: Surgical terminal Ctrl+Z guard

The workspace SHALL strip SUB (0x1A) at the outbound terminal-input chokepoint, log dropped byte counts and preserve every other byte in order. A payload consisting only of SUB SHALL issue no request and SHALL not disable input. Composer validation SHALL continue to reject control-character messages before any submit.

#### Scenario: Embedded suspend byte

- **WHEN** terminal input contains ordinary bytes and Ctrl+Z
- **THEN** the daemon receives only the remaining bytes in their original order
- **AND** Ctrl+C, Ctrl+Y and escape sequences remain unchanged

### Requirement: Browser attention parity

The workspace SHALL reuse the existing favicon link, showing /icon-192.png and the title Argus when no tasks need input. With N tasks needing input, the title SHALL be (N) Argus and the original icon SHALL retain its shield with a small count badge composited over it; failed image loading SHALL retain the plain original icon. Returning to zero SHALL restore the plain icon and title, including when image loading completes afterward. The workspace SHALL use the task rail as the sole task list. Needs-input alerts SHALL default on in the page, idle alerts SHALL default off. Desktop alerts SHALL use one explicit Web Push permission/subscription control; the page SHALL NOT construct a second desktop Notification. Notifications SHALL ignore focused tasks, dedupe pending task IDs until attention clears and post the first three distinct arrivals immediately within a rolling five-second window. Further arrivals SHALL update one summary. Snapshots SHALL update badges silently. Disconnect SHALL clear alerts, notifications, timers and badge counts. Needs-input and idle in-page preferences SHALL live in Settings Notifications using their existing persisted flags; the removed browser-banner preference SHALL be ignored. There SHALL be no separate attention task popover. Native Dock counts SHALL use tab badges; the native menu-bar extra is not applicable to a browser. Next-attention navigation SHALL respect current rail filters.

#### Scenario: Burst coalescing

- **WHEN** five distinct unfocused tasks need input within five seconds
- **THEN** the first three alerts appear immediately and the remaining two update one summary
- **AND** repeated events for a pending task add no alert

### Requirement: Account choice before task creation

New task creation SHALL offer backend-compatible accounts when multiple are available, using the project-scoped account roster and authoritative backend kind. It SHALL preselect the user's compatible choice or project default, omit account when no choice is offered, retain selection through failed submission and cancel late reads on close/project change. Task settings SHALL also consume the actual array-shaped accounts response for existing account movement.

#### Scenario: Create on a selected account

- **WHEN** a compatible named account is selected before creation
- **THEN** POST tasks includes that account without an additional account move or session restart
- **AND** changing backend removes incompatible choices

### Requirement: Retained task terminals

The workspace SHALL retain one terminal controller and its scrollback per visited task until that task disappears from the task snapshot or the connection is disposed, matching the native Mac cache. Task selection SHALL move the retained view without rebuilding its terminal. Hidden cached output streams SHALL be suspended to release browser connections; foreground resume SHALL append only unseen bytes from a cursor-fenced catchup snapshot, or replace with bounded replay when the process counter resets or history has been evicted. Pending input SHALL be canceled on task selection independently of the retained output stream. Global disconnect/pagehide SHALL dispose every controller and clear its Fit hint. The unified project grouping and coordinator rosters SHALL include hoisted freelance roles through their owning orchestrator ID.

#### Scenario: Return to a visited task

- **WHEN** the user selects another task and returns
- **THEN** the same terminal is shown with deduplicated cursor-based catchup, retaining scrollback when the replay window covers the saved cursor
- **AND** a send interrupted by the selection cannot submit its trailing Enter


### Requirement: Settled automatic terminal fit and a visible last column
Terminal pane changes SHALL fit automatically after approximately 400 ms of settled geometry, only when proposed cells differ from current PTY cells. Hidden, disconnected, recorded and cache-suspended terminals SHALL never trigger automatic resize. Known busy sessions SHALL defer automatic fitting; the client SHALL recheck existing REST task state before posting and retry eligibility when task state or visibility changes. Only one resize per terminal may be in flight, with newer geometry coalesced. Explicit Fit remains a fallback for a visible interactive terminal.

#### Scenario: Resize a visible idle terminal
- **WHEN** window, pane layout or task visibility changes settle
- **THEN** exactly one needed resize SHALL be posted after the debounce using existing REST, with additional changes coalesced behind an in-flight resize
- **AND** identical authoritative cell dimensions SHALL post no resize, reconciling a stale cached view locally instead
- **AND** pending or superseded work SHALL recheck visibility, task and geometry before mutation
- **AND** automatic failures or busy/unsupported states SHALL retain a manual mismatch hint rather than repeatedly retrying

#### Scenario: Preserve scrollbar and right-edge geometry
- **WHEN** a live or recorded terminal is displayed before or after fitting at supported desktop widths
- **THEN** xterm SHALL own the only vertical terminal scrollbar
- **AND** the final painted glyph SHALL be fully visible inside its renderer row and clear the scrollbar, reserving classic/overlay scrollbar space and a small glyph overhang gutter
- **AND** wider authoritative output SHALL remain horizontally reachable without document overflow or disabled accessibility

### Requirement: Derived rail attention emphasis

Needs-input tasks and their collapsed ancestors SHALL use bold, brighter names and a subtle semantic attention accent in the single project grouping and light/dark themes. The status icon SHALL remain. Selected attention rows SHALL remain distinct. Emphasis SHALL derive from current task/role attention signals and clear with them, without separate read state.

#### Scenario: Collapsed attention clears
- **WHEN** a descendant requires input beneath a collapsed project or coordinator
- **THEN** its row and ancestor rollup retain attention emphasis
- **AND** clearing the existing signal removes emphasis without a read action

### Requirement: Shared workspace operations

Header, task settings and Open PR SHALL share credential-free HTTP(S) PR resolution, preferring pr.url metadata and falling back to output links marked isPR. Output links SHALL share one read within a task selection. Backend and full-project rosters SHALL share cached reads with independent consumer cancellation, clone isolation, failed-read eviction and invalidation on settings writes or credential changes. Explicit project refresh SHALL bypass old roster results.

All confirmations SHALL use one text-only modal supporting Cancel, Escape, focus restoration, owning-section cancellation and async mutation errors. Clipboard operations SHALL share success/failure feedback while retaining credential-specific warnings. Creation and move SHALL share explicit account policies; moving SHALL remain Claude-only with a visible explanation. Task settings SHALL expose only one Resume agent start action using the resume endpoint, which also guards pending daemon rerender restarts. Copy worktree path SHALL be named explicitly in shortcut help.

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


### Requirement: Full-window Settings mode
Settings SHALL replace the entire optional workspace window, hiding the task navigation, task tools, separators and workspace header. The left Settings column SHALL contain Back, search with a shortcut hint, and the existing grouped section entries with icons. The main page SHALL have a large section title, a bounded readable width, and shared setting rows with labels and muted descriptions on the left, controls on the right and hairline separators. Complex section content SHALL retain its behavior inside the same page frame.

#### Scenario: Enter and leave Settings
- **WHEN** Settings opens from a task or review route
- **THEN** no task rail or review tools SHALL remain visible or focusable
- **AND** Back or Escape SHALL return to the exact prior task, view and review selection while retaining the task draft
- **AND** reloading a Settings deep link SHALL restore full-window Settings

#### Scenario: Compact Settings navigation
- **WHEN** the window is narrow
- **THEN** Settings navigation SHALL become a top Back and grouped section selector bar
- **AND** every existing section and repository SHALL remain reachable
- **AND** the page SHALL have no document horizontal overflow at 390, 1280 or 2544 pixels

This optional-client layout does not change the REST surface. The existing TUI and native macOS Settings presentations remain intentional client-specific layouts.

### Requirement: Compact Changes navigation

The Changes panel SHALL display one compact line per file, with a dim directory prefix and bright intact filename. Only the directory prefix SHALL ellipsize; exceptionally long filenames SHALL scroll inside the file list rather than expand the document. Exact added/removed text counts and a small status badge SHALL align to the right. The header SHALL show N files changed and total added/removed counts from the returned branch/working diff-stat summary lines. Per-file counts SHALL load only for visible rows through IntersectionObserver with at most four concurrent existing file-diff reads, cached per task/status generation and canceled on task changes. Pending and unavailable per-file counts SHALL remain blank, binary files SHALL show bin and directories SHALL retain lazy expansion. Per-file counts SHALL never be inferred from scaled stat-bar estimates.

Hover or keyboard focus SHALL reveal a small menu chevron using the shared task-menu popover behavior. The menu SHALL offer Copy path, Copy absolute path and an encoded vscode://file link for Open in VS Code when an absolute worktree is available. Copy actions SHALL use the shared copyNotice. The row SHALL still open its diff. No discard, Finder, Xcode or always-visible copy controls SHALL appear. A small icon toggle in the header SHALL switch flat/tree views with the same row styling and captured paths.

#### Scenario: Compact row actions

- **WHEN** the Changes panel renders files
- **THEN** no inline copy buttons SHALL be visible
- **AND** hover or keyboard focus SHALL reveal the row menu trigger
- **AND** keyboard menu navigation and Escape SHALL restore trigger focus
- **AND** each copy/editor action SHALL retain its row's relative or absolute path

#### Scenario: Refresh and tree counts

- **WHEN** the user switches between flat and tree presentation
- **THEN** filenames, status badges, counts and menu actions SHALL remain available without duplicate count reads
- **AND** changing task or refreshing SHALL cancel prior counts and reject late results
- **AND** the changed-file list SHALL reflect the returned API paths without substituting a frontend comparison base

### Requirement: Compact task header

The optional workspace SHALL place task identity, branch copy, available PR link, compact Agent/Review tabs and labeled Split/Fit icon buttons in one 40 to 48 pixel desktop bar. The task name SHALL use normal weight and ellipsis with the full name and project in its native title tooltip even when it fits. This identity tooltip is an intentional exception to delegated control tooltips and truncation-only full-value help because project information is otherwise hidden. The header SHALL omit the task eyebrow, project subtitle and permanent Live terminal toolbar. Terminal status SHALL remain accessible, and errors and reconnect actions SHALL remain visible when relevant. Fit SHALL remain a manual fallback alongside automatic fitting. Existing PR, branch copy and split keyboard actions SHALL retain their behavior. This visual change is specific to the optional workspace; TUI, original web and macOS headers are outside its scope.

#### Scenario: Compact and narrow task header
- **WHEN** a task is selected at 1280 or 2544 pixels
- **THEN** the task header SHALL occupy at most 48 pixels and the terminal SHALL begin directly below it
- **AND** every header action SHALL be keyboard reachable with an accessible name
- **WHEN** the viewport is 390 pixels wide
- **THEN** the header MAY wrap to two lines without horizontal document overflow

### Requirement: Aligned composer actions and discoverable controls

The optional workspace composer SHALL center its read-only agent identity and context, attachment and send targets on one axis. All targets SHALL be 32 pixels tall with centered 16 pixel glyphs, including pending send, and the footer SHALL not introduce an internal separator. Composer, header and terminal controls SHALL use the shared delegated data-tooltip contract with accessible names and no competing native title. Context help SHALL reflect measured usage or its unavailability. Split help SHALL include its shortcut and current open/close action; Fit help SHALL preserve proposed/current dimensions.

#### Scenario: Consistent composer geometry
- **WHEN** a task is shown at 1280, 2544 or 390 pixels
- **THEN** the identity and action centers SHALL differ by no more than one pixel
- **AND** controls SHALL retain equal height without horizontal document overflow

#### Scenario: Discover control purpose
- **WHEN** a user hovers or focuses an eligible composer, task header or terminal control
- **THEN** its shared tooltip SHALL explain the action with an accessible name
- **AND** native title tooltips SHALL not compete with that help

### Requirement: Review controls use shared workspace tooltips

The optional workspace SHALL expose shared data-tooltip text and accessible names on Changes view icons, file menu triggers, status badges, line counts and task menu triggers. File statuses SHALL expand to Added, Modified, Deleted, Untracked or Renamed; text counts SHALL state the exact added and removed totals. Full paths and truncated diff/artifact/task names, including split picker selections and coordinator roster rows, SHALL retain their full values through shared tooltips. Controls with tooltips SHALL omit native title attributes. Existing visible text labels on diff modes, artifact actions, dialog actions and shortcut Close SHALL remain sufficient without redundant tooltips. This presentation applies only to the optional workspace; the other clients retain their existing control help.

#### Scenario: Inspect review controls

- **WHEN** a user hovers or focuses a review icon or a truncated review label
- **THEN** the shared delegated tooltip SHALL explain the icon or reveal the full value
- **AND** rerendered flat/tree rows and refreshed line counts SHALL expose current tooltip attributes without native tooltip duplication

### Requirement: Shared tooltips and compact rail footer

The optional workspace SHALL use one shared delegated tooltip bubble for data-tooltip elements, with a separate aria-label on icon-only controls and no native title on opted-in controls. Hover SHALL wait 400 ms, focus-visible SHALL show immediately, optional data-tooltip-shortcut SHALL render a dim key hint, and Escape, leave, blur, scroll and click SHALL hide it. The bubble SHALL stay in the viewport, use role=tooltip and preserve existing aria-describedby values. Motion SHALL respect reduced-motion preferences and surfaces SHALL use theme tokens.

The rail footer SHALL contain one horizontal line with a live connection-state icon, keyboard-shortcuts icon and Settings gear. All icon-only rail controls and status indicators SHALL expose data-tooltip and aria-label. This requirement applies only to the optional workspace, with no REST or other frontend changes.

#### Scenario: Dynamic rail control tooltip
- **WHEN** a dynamically rendered rail icon is hovered or keyboard focused
- **THEN** one reusable bubble SHALL describe its action or current status
- **AND** any shortcut hint SHALL remain separate from its accessible name
- **AND** the footer SHALL remain one line at 1280 and 390 pixel viewport widths

#### Scenario: Nested tooltip owners
- **WHEN** the pointer leaves a nested count or status tooltip owner for its parent path owner
- **THEN** the parent tooltip SHALL appear after the hover delay and the nested owner's aria-describedby SHALL be removed
- **AND** entering the nested owner again SHALL replace the parent's tooltip
- **AND** movement between plain descendants of the same owner SHALL retain its visible tooltip

### Requirement: Rail hierarchy geometry
The optional workspace SHALL place project tasks one visible level below their project and coordinator descendants one consistent 12 pixel indent farther in. Root task icons SHALL align with project avatars within 2 pixels and sit no more than 20 pixels past the project chevron. Each child container SHALL contribute its level indent once, with the root container also honoring the project header inset. Parent chevrons SHALL use the same rotating 12 pixel SVG as project disclosures in a slot inside each row background, with a 4 pixel gap before the status icon and no overlap with the selected accent bar. Leaf rows SHALL reserve the same slot without changing icon columns or the 12 pixel nesting step. Enter, Space and pointer activation SHALL toggle without selecting the row; ArrowRight and ArrowLeft SHALL expand and collapse. Disclosure tooltips SHALL add the current action and direct child task count, rather than repeating the task name. Project action buttons SHALL share a 24 pixel box and centered 16 pixel SVG glyphs with matching stroke. Task icons and role tags SHALL be vertically centered, with role tags sharing a right edge regardless of nesting or pins. Selected row backgrounds SHALL enclose the disclosure slot and extend to the project right edge. Existing attention, tooltip, keyboard, search, archive and pin behavior SHALL remain unchanged.

#### Scenario: Responsive rail nesting
- **WHEN** a project with a coordinator, worker siblings and ordinary tasks is expanded at 1280 or 390 pixels in either theme
- **THEN** root icons SHALL align under the project avatar within 2 pixels, no more than 20 pixels past the project chevron, and sibling icons SHALL share a column
- **AND** each descendant icon SHALL move right by 12 pixels (between 10 and 14 pixels), with its disclosure gutter inside its own row background
- **AND** project actions SHALL share dimensions and vertical centers while role tags share a right edge

#### Scenario: Disclosure geometry and activation
- **WHEN** selected and unselected parent rows render at 1280 or 390 pixels in light or dark themes
- **THEN** the disclosure SVG SHALL center within 1 pixel of the task text line and remain fully inside the row background beyond its accent bar
- **AND** its right edge SHALL sit 4 pixels before the status icon while parent and leaf icon columns stay aligned
- **WHEN** the disclosure receives a pointer click, Enter, Space, ArrowRight or ArrowLeft
- **THEN** descendants SHALL toggle, expand or collapse without changing the selected task

### Requirement: Informative tooltips only

Tooltips SHALL appear only for icon-only controls, visibly truncated names/paths, or information beyond the visible label. The shared handler SHALL suppress whitespace-normalized duplicate labels when the owner and its ellipsis-bearing text child fit. Labeled controls with shortcut hints SHALL show only the shortcut. Task names, repository names, file paths and other full-value labels SHALL reveal their full value only while visibly truncated. Labeled controls SHALL use their visible text as their accessible name. This refinement is limited to the optional workspace and changes no REST contract or other client.

#### Scenario: Redundant Settings label
- **WHEN** the user hovers the visible Settings System navigation label
- **THEN** no tooltip SHALL appear

#### Scenario: Truncated task name and shortcut
- **WHEN** a task name is truncated
- **THEN** its tooltip SHALL reveal the full name
- **AND** the same name SHALL have no tooltip when it fits
- **AND** a labeled control with a shortcut SHALL show the shortcut alone

### Requirement: Visible task tools update live
The optional workspace SHALL refresh Changes and Artifacts for the selected task on session.idle, session.needs_input, session.exited and task.status_changed events, on visibility/focus return, and on a ten-second fallback while the pane and focused document are visible. Reads SHALL be single-flight with coalesced triggers and cancellation/fencing on task switch. Changes SHALL avoid rendering identical path/status/summary results and preserve scroll, view mode, folder expansion, row menu and opened review across updates. Path count caches SHALL invalidate only affected status/stat summaries. The normal Changes header SHALL have no Refresh control; empty state SHALL say No changes yet, and read failures SHALL offer Retry.

#### Scenario: Agent writes a file
- **WHEN** the selected task emits session.idle while Changes is visible
- **THEN** a fresh git/status read displays the file without user action

#### Scenario: No write event exists
- **WHEN** a visible focused Changes pane remains open without an SSE write signal
- **THEN** a ten-second fallback discovers changes without polling hidden panes or background windows

### Requirement: Attachment submission observes recipient readiness

When a recognized Claude-style composer echoes an uploaded image path, the client SHALL wait for that path to finish converting before Enter. Attachment sends SHALL wait for recipient output to advance after paste and remain quiet for 100 milliseconds before sending standalone CR, with a five-second paste-settle bound. They SHALL retain draft and attachment references until submission acknowledgment. Identifiable composer content SHALL clear after CR before acknowledging success; unrelated output with unchanged input SHALL not count. An unknown composer MAY use fresh output as a conditional acknowledgment fallback, without semantic guarantees or automatic CR retries. At most one CR-only retry SHALL occur, only when the exact observed nonempty composer remains unchanged. Task changes and timeout SHALL cancel remaining submit work and retain the captured task draft. Explicit disconnect SHALL cancel submit work and keep its existing credential/draft clearing behavior. Plain-text sending SHALL retain its existing behavior.

#### Scenario: Slow image ingestion
- **WHEN** a recipient takes longer than 50 milliseconds to consume an attached image path
- **THEN** CR SHALL follow observed output settling outside the closed paste block
- **AND** an early echo of an uploaded image path SHALL not count as completed image ingestion
- **AND** the recipient SHALL accept exactly one message before the browser clears the draft

#### Scenario: Swallowed Enter
- **WHEN** the first CR leaves an identifiable composer unchanged
- **THEN** the client MAY retry CR once without repeating message text
- **AND** changed input, unsupported layouts or cancellation SHALL prevent that retry
- **AND** persistent uncertainty SHALL leave the browser draft and attachments available

#### Scenario: Enforce the development proxy trust boundary
- **WHEN** requests reach the loopback development server
- **THEN** invalid Host and cross-site fetch requests SHALL be rejected, local navigation SHALL remain supported and static assets SHALL follow the allowlist
- **AND** proxy forwarding SHALL strip cookies, Set-Cookie and hop-by-hop headers while preserving required API response headers

#### Scenario: Bound registered artifact previews
- **WHEN** an artifact path traverses outside its manifest prefix or its preview exceeds the preview size cap
- **THEN** the client SHALL reject it and cancel an oversized streaming read, including without Content-Length
- **AND** permitted bounded previews SHALL preserve exact bytes and MIME type

#### Scenario: Open a push attention target safely
- **WHEN** the workspace push worker receives a notification click with a task identifier
- **THEN** it SHALL open an encoded same-origin task URL without interpreting the identifier as a URL or credential query
- **AND** malformed push data SHALL use safe notification defaults and the worker SHALL not intercept fetches

#### Scenario: Refuse multiline input in a plain shell
- **WHEN** composed text contains multiple lines and the recipient lacks bracketed paste mode
- **THEN** sending SHALL fail before writing any input bytes
- **AND** a supported bracketed paste recipient SHALL receive protected text followed by a separate submit

#### Scenario: Preserve schedule edits and unavailable measurements
- **WHEN** an existing one-shot schedule is edited without changing its cadence
- **THEN** its request SHALL omit unchanged cadence and preserve the existing one-shot time
- **AND** System measurements SHALL distinguish unavailable values from measured zero
