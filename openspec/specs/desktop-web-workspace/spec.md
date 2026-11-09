# desktop-web-workspace Specification

## Purpose
Define Theia, the optional standalone Argus web workspace using existing authenticated REST and SSE operations, including task navigation, terminals, review tools, Settings and client state protections.

## Non-Goals and named follow-ups

The optional web workspace is the client we converge on. This specification is the source for a future Mac app rebuild. The workspace uses existing authenticated REST and SSE contracts. The following canonical register records remaining frontend parity and backend gaps, not delivered capabilities. Historical exclusions remain recorded in the [original proposal](../../changes/archive/2026-11-07-add-desktop-web-workspace/proposal.md) and subsequent archived amendments.

### Client convergence

- `native-workspace-rebuild` rebuilds the Mac client from this specification, covering the unified nested project rail, arbitrary task split, compact header, retained terminals, composer and attachments, provider suggestion hints, measured context, archive context retention, Changes and split diffs, registered artifacts, full Settings breadth, live tools, attention and accessibility. Native credentials, notifications, Finder and window integration remain platform choices.
- `tui-workspace-interaction-parity` evaluates the unified task hierarchy, arbitrary split, per-task draft and attachment composer, provider suggestion hints, measured context, archive viewer retention, resize policy, surgical Ctrl+Z handling, attention rollups, live review updates and corresponding accessible controls for the TUI. Each difference receives an implementation or explicit terminal-specific disposition.
- `legacy-spa-workspace-convergence` decides replacement or selective porting of workspace navigation/history, layout, composer and submission behavior, review controls, retained terminals, attention, settings navigation and accessibility into the original SPA. The original SPA remains available during this transition.
- `workspace-plugin-settings` evaluates existing plugin forms and bridge-budget editing through supported operations.
- `workspace-backend-management` evaluates existing master-only backend CRUD and permission defaults. Agents and accounts remain read-only in this change.
- `workspace-bridge-controls` evaluates existing bridge configuration controls for workspace Settings.

The native `add-web-hera-mutations` follow-up remains separate. Spawn, new coordinator, move, join, detach, consuming agent inbox and merge-safety Cleanup retain their standing TUI-only disposition. The workspace rail supports coordinator kanban and orchestrator pin mutations through existing endpoints. One unified project rail replaces separate Tasks/Hera modes, the Hera visibility filter and a separate Freelance section. Task status remains editable in Settings rather than through quick workflow transitions. Archive/restore remains in the rail. Session selection remains beside Stop/Resume. Web Push is the desktop-alert channel; in-page attention preferences remain separate.

Browser drawers, DOM tooltips and their pixel geometry do not require literal native or terminal copies. Path copy and encoded VS Code links replace native Finder/Xcode launching. Opt-out origin token storage replaces Keychain. Tab title/favicon attention and the unified rail replace Dock/menu-bar affordances. Native window controls remain platform-specific. These equivalents add no backend requirements.

### Settings interaction follow-up

- `workspace-settings-unsaved-edit-confirmation` adds a leave confirmation for unsaved Settings edits, shared by Back, Escape, task selection and surface shortcuts. Current departures discard unsaved edits without saving and refuse while a save is in flight. This follow-up does not add a dialog in this delivery.

- `workspace-task-bulk-actions-parity` covers selected-ID bulk archive/delete in the TUI, original web and macOS clients.
- `workspace-rail-selection-accelerators` covers optional modifier and range selection.

### Backend contracts deferred

The numbered references retain the original backend tally identifiers and record remaining scope.

1. `task-runtime-state-all-statuses` exposes live/idle state for every task with a session. Until then, activity outside in_progress is incomplete.
2. `task-failure-state` exposes task failure independently of workflow status. Until then, only failed Hera roles support a failure indicator.
3. `distinct-pr-terminal-states` separates merged PRs from unmerged closure. The current indicator accurately combines them.
4. `preserve-project-diligence-profile` preserves profile assignment on REST project edits. The current editor warns about the inherited loss.
5. `rest-account-management` defines authenticated account CRUD. Existing account selection does not imply account management.
6. `safe-runtime-model-switch` defines model changes for running tasks. The composer model label remains read-only.
7. `provider-context-usage-contract` exposes remaining provider identity, Codex quota/account details beyond measured context and session/turn usage. Unknown fields remain omitted and historical totals stay separate.
9. `targeted-task-runtime-edits` defines safe backend, model, sandbox and prompt transitions. Generic full-record PUT is not a runtime editor.
10. `cross-backend-account-move` defines account movement beyond Claude. Existing movement remains Claude-only.
11. `durable-task-pr-association` defines manual PR links that coexist with the branch poller. This client reads existing PR links only.
14. `multiplexed-terminal-output` provides continuous shared output across visited tasks. Hidden cached terminals currently suspend SSE and recover only the bounded existing replay window.
15. `discard-worktree-file` defines a confirmed per-file discard operation. Changes omits Discard.
16. `git-per-file-numstat` supplies numeric per-file counts. Visible rows currently fetch individual diffs with at most four concurrent reads.
17. `task-base-aware-git-comparison` honors task.base_branch and supplies comparison provenance. Changes currently displays the daemon's whole-stack comparison without client filtering.
18. `worktree-change-events` supplies authoritative worktree updates. Changes and Artifacts retain relevant session-event refresh, focus/visibility refresh and a visible-only ten-second fallback.
19. `clear-stale-role-ready-marker` clears ready_to_close when a role resumes working. Workspace role tags currently give working precedence over the stale marker.

20. `codex-recapture-test-env-isolation` isolates CODEX_HOME and CODEX_SQLITE_HOME in the daemon recapture fixture. This is a pre-existing test issue on master, not a product defect.

Named backend follow-up `reject-archived-task-input` makes daemon handleWriteInput reject archived tasks. The frontend does not fence input during a pure event-stream outage without an archive envelope.

The broader existing backend exclusions also have names. `conditional-pty-input` checks input against process identity at the PTY owner. `identified-git-outcomes` defines comparison identity and explicit missing-worktree, git-failure and stale-snapshot outcomes. `task-shell-and-source-browsing` defines independent task shells and bounded repository content reads. `provider-conversation-feeds` defines verified provider conversation rendering. `durable-review-feedback` ties feedback to comparison identity and external review synchronization. None is implemented here. Client request fencing, attachment readiness observation and settled terminal fitting do not strengthen these backend guarantees.
## Requirements
### Requirement: Standalone additive workspace
The workspace SHALL run as a separate loopback frontend using existing authenticated REST and SSE operations through its development proxy. It SHALL NOT require Go, daemon, existing static shell, REST contract, schema or backend behavior changes. The existing web UI SHALL remain available and unchanged.

#### Scenario: Run alongside the existing UI
- **WHEN** the standalone client is started
- **THEN** it SHALL be served on its own loopback port and forward existing authenticated `/api/` operations with proxy security filtering to one configured loopback daemon
- **AND** response headers including `X-Output-Total` SHALL remain available to the client
- **AND** the existing web UI and daemon remain untouched

#### Scenario: Preserve application identity
- **WHEN** the optional workspace loads
- **THEN** the tab title SHALL be Theia and the header SHALL display the selected Spectrum eye mark at 25 pixels beside the Theia wordmark, with no workspace edition label. The mark SHALL be decorative and the brand accessible name SHALL remain Theia
- **AND** the workspace SHALL serve its own /theia-favicon.svg, /theia-icon-32.png, /theia-icon-192.png and /theia-apple-touch-icon.png (180 pixels) read-only, using these assets for favicon, touch and notification icons without changing the original Argus assets
- **AND** compact header widths SHALL retain their existing responsive visibility and fit without horizontal overflow

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

### Requirement: Workspace document isolation
The optional workspace proxy SHALL apply Content-Security-Policy sandbox without permissions and X-Content-Type-Options nosniff to every daemon API response, preserving existing upstream CSP restrictions. Workspace HTML shells SHALL deny framing with frame-ancestors 'none' and X-Frame-Options DENY. These requirements apply to the optional Node workspace only; daemon, TUI and macOS behavior remains unchanged.

#### Scenario: Navigate to active raw artifact content
- **WHEN** a user follows a raw artifact link, including HTML or SVG, with a remembered workspace token
- **THEN** the document SHALL NOT execute scripts or access workspace origin storage
- **AND** fetched Review previews and artifact downloads SHALL retain their bytes and MIME types

#### Scenario: Frame the remembered workspace from another loopback port
- **WHEN** another same-site origin embeds a workspace HTML shell
- **THEN** the browser SHALL refuse to display the shell in that frame

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
- **AND** explicit send SHALL include ready attachment paths as space-separated tokens, escaping whitespace, quotes, backslashes and shell-significant characters with backslashes as terminal file drag-and-drop does, leaving simple paths unchanged and preserving the user text without an upload-triggered submission
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

### Requirement: Task mutations invalidate the rail through SSE. Replace full raw pin writes with targeted POST /pin.

Task invalidation events coalesce authoritative snapshots. Subscribe before snapshots, retain buffered events across failed reads, and resync on reconnect, unknown envelopes and visibility return. Keep the 30 second visible-document task poll until the first task.updated event, then stop it. Reconnect resets event-support observation and restores the fallback until another task.updated. Retry connection remains a recovery path for persisted-event failures. Pin posts only pinned and applies returned pinned/archived flags, including restoration. Task snapshots SHALL reconcile present boolean pinned fields on initial load, reconnect, resync and fallback polls, retaining cached pins when older daemons omit the field. Snapshots started before a newer local or SSE pin observation SHALL NOT regress that observation. Pin invalidations SHALL read the single-task DTO behind connection and per-task cancellation fences, without extra raw, selection or menu reads.

#### Scenario: Obsolete responses

- **WHEN** a task or connection changes while a request is pending
- **THEN** late responses cannot overwrite the current view or draft

#### Scenario: Unavailable API

- **WHEN** a read or mutation fails
- **THEN** the view shows actionable retry feedback without claiming success

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
- **THEN** active non-idle sessions SHALL show the Scribe, a pen nib writing three short lines, lifting and returning to the start over a 5.6 second loop in the existing fixed 16px icon slot, using currentColor from the rail text token with 1.4px rounded strokes and only transform, opacity and stroke-dashoffset animation, and idle/pending/offline tasks without a PR SHALL show a gray branch icon
- **AND** open/review PRs SHALL show a green pull-request icon, approved PRs a distinguishable green variant, complete tasks a purple merged icon, and needs-input a prominent attention icon
- **AND** failed bound Hera roles SHALL show red failure icons
- **AND** reduced-motion preference SHALL stop all working indicator animation and retain the completed three lines and stationary nib as the static working frame
- **AND** the combined REST `merged-closed` state SHALL use a purple terminal PR icon labeled PR merged or closed without claiming it can distinguish the two
- **AND** task failures outside the Hera roster remain unavailable because the task REST payload has no failure field

#### Scenario: Busy Hera worker already in review
- **WHEN** a session is live and not idle, including a task in review or complete workflow status
- **THEN** the working Scribe SHALL represent its activity regardless of workflow status, retaining archived, needs-input, failure and combined terminal PR precedence
- **AND** an idle task in review without cached PR state SHALL remain gray and labeled In review, since workflow status alone does not establish a PR

### Requirement: Conductor composer layout
The optional workspace SHALL show a large rounded draft box with no default placeholder text, except a verified provider suggestion while the draft is empty under Empty composer suggestion. Backend/model identity SHALL be read-only; context, attach and send controls SHALL be aligned on the right. A file picker SHALL reuse the existing upload path and attachment constraints. Attachment instructions SHALL appear only for errors, while chips and existing paste/drop behavior remain.

#### Scenario: Native composer actions
- **WHEN** a task is selected and files are chosen with the attach button
- **THEN** uploads belong to that task and no terminal input is sent until explicit send
- **AND** native paste, dictation, IME and per-task drafts preserve existing behavior

### Requirement: Measured context popover
The context control SHALL open an accessible dismissible popover, show only sourced existing REST values with their correct scope, and indicate unavailable context without fabricating a fill or percent. The popover SHALL retain Claude plan, email, organization and usage limit windows when available, while Account and historical role token totals SHALL remain in task Info and SHALL NOT appear in the composer popover. The composer SHALL NOT fetch the Hera roster for context readings. Unsupported backend fields SHALL be omitted. Switching tasks and disconnecting SHALL cancel reads and reject late results. Bridge and Hera metadata context readings SHALL be fallback only when the task context route is unsupported.

#### Scenario: Backend measurements are partial
- **WHEN** REST exposes account usage but no context-window measurement
- **THEN** usage is shown only for the task's matching account and backend
- **AND** the context ring remains unknown rather than displaying quota as context
#### Scenario: Legacy bridge and recorded stamp availability
- **WHEN** the daemon lacks the task context route and the selected task has a live bridge context with a measured percent or measured tokens/window
- **THEN** the ring displays that context percentage and the popover shows available token/window values
- **WHEN** that older daemon has only Hera metadata
- **THEN** the popover labels the values as recorded, shows record time and pairs percent with tokens only for equal timestamps
- **AND** a configured recycle budget and historical role token totals never supply an inferred window

#### Scenario: Shared account across backends
- **WHEN** a Codex task uses an account that also supports Claude
- **THEN** the composer omits the Account row and task Info retains the raw account identity
- **AND** Claude plan, email and account quotas SHALL NOT be attributed to Codex
- **AND** unsupported Codex fields remain absent as documented in web-workspace/docs/context-data.md

#### Scenario: Historical token totals
- **WHEN** a task has exactly one live bound role with measured REST token totals
- **THEN** available counts remain in task Info under its cost display rules and SHALL NOT appear in the composer popover
- **AND** ambiguous multiple role bindings are not summed or arbitrarily selected

#### Scenario: Composer keeps distinct Claude account details
- **WHEN** the matching Claude account exposes plan, email, organization and valid usage limit windows
- **THEN** those values remain in the composer with measured context and reset times
- **AND** the composer omits Account and historical role token totals without reading the Hera roster

### Requirement: Project rows and task creation
Project headers SHALL show a letter avatar and project name, an always-visible New task (+) button and a hover/focus Configuration gear. Project headers SHALL omit task counts. Empty projects SHALL have no disclosure control, expanded state or empty child area, including when previously expanded projects become empty after refresh or archive filters. Non-empty groups SHALL retain existing pointer, keyboard and in-memory disclosure behavior. The Configuration gear SHALL open the project editor directly; no single-item project menu or duplicate New task action SHALL remain.

#### Scenario: Start a task from a project
- **WHEN** New task is chosen from a project's plus button
- **THEN** the main pane SHALL open a task-creation form with that project preselected, a native prompt field and optional name
- **AND** POST /api/tasks SHALL use existing JSON fields and omit backend and account overrides unless explicitly selected, omit branch overrides, and omit blank model and inherited sandbox overrides
- **AND** successful creation SHALL select the new task and open its agent terminal
- **AND** prompt paste SHALL preserve multiline text without submitting; ordinary Enter SHALL submit and Shift+Enter SHALL insert a newline, retaining composition guards
- **AND** errors SHALL show inline while keeping the prompt and name for correction or retry
- **AND** Escape or Cancel SHALL return to the previous workspace view without sending agent input
- **AND** canceling an in-flight request SHALL ignore late results and explain that creation may have completed
- **AND** creation drafts SHALL stay in tab memory and clear on disconnect; pre-creation attachments remain outside scope, with task attachments available after creation

### Requirement: Task creation model and sandbox
The optional workspace SHALL expose model and sandbox choices only during task creation, using existing backend metadata and POST /api/tasks fields.

#### Scenario: Choose creation model and sandbox
- **WHEN** the task creation form opens
- **THEN** Model SHALL show only Daemon default and remain disabled while Backend is inherited, regardless of project or global backend metadata, because routing can select another backend
- **AND** choosing an explicit backend with a nonempty models list SHALL offer only Backend default and listed identifiers, rejecting unlisted identifiers
- **AND** choosing an explicit backend without a models list SHALL allow trimmed free text with a Backend default placeholder
- **AND** changing Backend SHALL reset Model to default and refresh its control and choices
- **AND** Sandbox SHALL offer Project default, Enabled and Disabled with Project default selected initially
- **AND** POST /api/tasks SHALL include every explicitly selected nonempty model, including one equal to the backend default, omit blank model, and include sandbox_override only as enabled or disabled when explicitly selected
- **AND** each project draft SHALL preserve both choices across cancellation and failed creation, clearing on successful creation or disconnect
- **AND** both controls SHALL have accessible names, visible keyboard focus when enabled and be disabled while creation is pending

### Requirement: Settings home and project management
The footer SHALL replace its original UI link with an accessible settings gear beside connection status. Settings SHALL retain the workspace's task draft and terminal attachment, support General/repository section navigation and return through Escape, Back, task selection or surface shortcuts. Departure SHALL refuse while a settings save is in flight, matching Back. Unsaved edits SHALL be discarded without saving on departure. Leave confirmation remains deferred to `workspace-settings-unsaved-edit-confirmation` in the canonical follow-up register above. Closing or disconnecting SHALL cancel stale settings reads and guard late results.

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
Project Configuration gears SHALL open the existing Settings project editor directly and SHALL be hidden when no configuration action is available. Repository navigation SHALL own the only project list and Add repository entry; the Projects overview SHALL be removed. Task rows SHALL offer a hover/focus actions button left of the existing archive icon, without selecting the row. Its menu SHALL offer Task settings, explicit Pin/Unpin and copying available task ID, branch and worktree values, followed by a separated red Delete action in Active and Archived. Archive/restore SHALL remain on the rail, in task settings and on the shortcut; the header and menu archive actions SHALL be removed. Every action menu item SHALL have exactly one leading decorative inline SVG icon from the shared monochrome stroke set, in a fixed aligned column, inheriting its label color. Destructive items SHALL have red text and icons and a preceding divider. Settings navigation entries SHALL use the same icon set, including Add repository, while repository identity avatars SHALL retain their letters. Labels, accessible names and keyboard behavior SHALL remain unchanged. Native form select options SHALL retain browser rendering. Menus SHALL support keyboard dismissal, focus restoration and periodic rail refresh without losing their target.

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

#### Scenario: Consistent icons in every action menu
- **WHEN** a task, coordinator or Changes file menu is rendered, including disabled and link items
- **THEN** every item SHALL contain exactly one leading aria-hidden SVG with shared size and stroke
- **AND** destructive items SHALL inherit red for their icon and label and have a preceding separator
- **AND** light or dark color tokens and a 220px rail SHALL preserve aligned labels without clipping
- **AND** keyboard arrows, Home/End, Escape and focus restoration SHALL retain their existing behavior

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

Role tags SHALL show blocked or failed first, then working, then ready when the role carries ready_to_close, then the role status. A working role with a live, non-idle bound task SHALL take precedence over a retained ready_to_close mark, and when a task has several live bindings the rail SHALL pick the binding by the same precedence. A row's working tag SHALL require its bound task to be live and not idle, because a self-reported working status never resets on its own. Otherwise the tag SHALL show ready when the role carries ready_to_close, idle while the session is live and stopped when it is not or the bound task is missing.

#### Scenario: Ignore obsolete rail preferences

- **WHEN** the workspace loads with a stored orchestration grouping preference
- **THEN** project groups with coordinator nesting SHALL render
- **AND** no grouping control, grouping shortcut or Hera task visibility filter SHALL appear
- **AND** split, role tags, search, archive and pin SHALL remain available

#### Scenario: Resumed worker does not show ready

- **WHEN** a live role reports working with ready_to_close set while its bound task is live and not idle
- **THEN** its role tag SHALL show working, not ready
- **AND** a done or idle role with ready_to_close SHALL still show ready

#### Scenario: Stale working status on an idle session

- **WHEN** a live role still reports working while its bound task is live and idle
- **THEN** its row tag SHALL show idle, or ready when the role carries ready_to_close
- **AND** the tag SHALL show working again once the session is running

#### Scenario: Missing bound task

- **WHEN** a live role reports working but its bound task is absent from the task snapshot
- **THEN** its tag SHALL show stopped, or ready when ready_to_close is set
- **AND** an unavailable split roster row SHALL remain disabled

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
- **AND** closing and reopening split reuses the retained secondary terminal unless intervening navigation evicted it under the terminal cache budget
- **AND** selecting a task never enables split implicitly

### Requirement: Task rail quick actions

The workspace SHALL expose task settings, explicit Pin/Unpin and copies through task row menus, reachable by context click and keyboard. Workflow status SHALL be edited only through the task settings select. Ordinary task Pin SHALL consume supplied boolean task state without another selection raw read, retain cached state when an older daemon omits pinned, POST only the desired pinned flag to the targeted pin endpoint and apply returned pinned/archived state, including restoration. Coordinator Pin/Unpin SHALL instead use the orchestrator's pin state and targeted authenticated endpoints without full task-record writes. All live Hera-bound tasks, including freelance bindings, SHALL remain available without a separate visibility filter.

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

### Requirement: Surgical terminal Ctrl+Z guard

The workspace SHALL strip SUB (0x1A) at the outbound terminal-input chokepoint, log dropped byte counts and preserve every other byte in order, subject to the Claude-family focus-out guard. A payload consisting only of SUB SHALL issue no request and SHALL not disable input. Composer validation SHALL continue to reject control-character messages before any submit.

#### Scenario: Embedded suspend byte

- **WHEN** terminal input contains ordinary bytes and Ctrl+Z
- **THEN** the daemon receives only the remaining bytes in their original order
- **AND** Ctrl+C, Ctrl+Y and escape sequences remain unchanged except the exact Claude-family focus-out payload

### Requirement: Terminal focus-out reports stay in the browser

The workspace SHALL prefetch the task's backend family when terminal input is attached, outside the serialized input path. A matched backend roster entry with kind `claude` or an explicitly empty kind `""` (the custom family on the wire) SHALL suppress an exact focus-out payload ESC [ O, matching the daemon's Claude-family session capabilities. It SHALL log the drop and issue no input request. Other resolved backend kinds, missing kind fields or missing backend matches SHALL forward focus-out unchanged. Focus-in reports and every other payload SHALL retain their bytes subject to the surgical Ctrl+Z guard. Leaving a Claude-family terminal for the composer SHALL not report blur, which suppresses Claude Code's suggested next prompt.

While classification is pending or unknown after failure, the workspace SHALL hold at most one exact focus-out report for at most two seconds, without awaiting classification on the input path. Repeated reports without intervening input SHALL NOT extend the deadline. Every other byte SHALL flow through the existing input queue immediately and in order. Any subsequently posted input SHALL mark the held report stale, and a newer focus-out MAY replace that stale report. When classification settles, the client SHALL discard held reports for Claude-family backends. For other resolved families, or an unresolved family after the two-second deadline, it SHALL forward the held report only if no input was posted after it was held, otherwise discard it as stale.

A failed prefetch SHALL log the failure and retry asynchronously after 250 ms and then one second, for at most three total attempts, adopting any successful shared API cache result. Keystrokes SHALL NOT trigger retries. Task-switch cancellation SHALL fence classification publication after the awaited read, cancel retry and hold timers, and discard held reports without canceling a shared API roster read needed by the next task.

#### Scenario: Composer focus after the terminal

- **WHEN** a resolved Claude-family agent enabled focus reporting and the terminal loses focus to the composer
- **THEN** no input request is sent for the focus-out report
- **AND** a later focus-in report is still forwarded

#### Scenario: Non-Claude focus handling

- **WHEN** a terminal for another resolved backend kind emits ESC [ O
- **THEN** the focus-out report SHALL reach the daemon unchanged
- **AND** a resolved roster with a missing backend match or kind field SHALL also preserve the report

#### Scenario: Classification is pending during input

- **WHEN** the backend roster read is pending and focus-out, ordinary bytes, Ctrl+C and composer input arrive
- **THEN** focus-out SHALL be held without delaying the other input
- **AND** the other input SHALL retain its order and make the held report stale
- **AND** once classification resolves, later focus-out reports SHALL follow the resolved family policy

#### Scenario: Cold live-stream focus reporting and composer blur

- **WHEN** a cold roster is pending while the first live output frame enables focus reporting or the interactive terminal loses focus to the composer
- **THEN** generated focus-out reports SHALL remain held in the browser
- **AND** Claude-family classification SHALL discard them
- **AND** initial snapshot replay SHALL retain its existing disabled-input fence

#### Scenario: Held report settles or reaches its deadline

- **WHEN** an exact focus-out was held and no later input was posted
- **THEN** Claude-family classification SHALL discard it and non-Claude classification SHALL forward it once
- **AND** an unresolved classification SHALL forward it once after two seconds
- **AND** stale reports or reports held by a detached input SHALL never be forwarded

#### Scenario: Transient classification failure

- **WHEN** the first prefetch fails and a later consumer successfully warms the shared backend cache
- **THEN** the next bounded asynchronous retry SHALL adopt that result for the same attached input
- **AND** ordinary input SHALL remain responsive throughout failure and retry
- **AND** repeated failures SHALL stop after three total attempts until input is reattached

#### Scenario: Task switch during shared classification

- **WHEN** a task is detached while its backend read is shared with the next task, including between promise fulfillment and continuation
- **THEN** the canceled task SHALL neither publish classification nor post late held input
- **AND** the next task SHALL continue using the shared read and its own backend match

### Requirement: Browser attention parity

The workspace SHALL reuse its existing favicon links, showing the Theia Spectrum eye SVG with 32 and 192 pixel PNG fallbacks and the title Theia when no tasks need input. With N tasks needing input, the title SHALL be (N) Theia and the Theia icon SHALL retain its eye with a small count badge composited over it; failed image loading SHALL retain the plain Theia icon. Returning to zero SHALL restore the plain icon and title, including when image loading completes afterward. The workspace SHALL use the task rail as the sole task list. Needs-input alerts SHALL default on in the page, idle alerts SHALL default off. Desktop alerts SHALL use one explicit Web Push permission/subscription control; the page SHALL NOT construct a second desktop Notification. Notifications SHALL ignore focused tasks, dedupe pending task IDs until attention clears and post the first three distinct arrivals immediately within a rolling five-second window. Further arrivals SHALL update one summary. Snapshots SHALL update badges silently. Disconnect SHALL clear alerts, notifications, timers and badge counts. Needs-input and idle in-page preferences SHALL live in Settings Notifications using their existing persisted flags; the removed browser-banner preference SHALL be ignored. There SHALL be no separate attention task popover. Native Dock counts SHALL use tab badges; the native menu-bar extra is not applicable to a browser. Next-attention navigation SHALL respect current rail filters.

#### Scenario: Burst coalescing

- **WHEN** five distinct unfocused tasks need input within five seconds
- **THEN** the first three alerts appear immediately and the remaining two update one summary
- **AND** repeated events for a pending task add no alert

#### Scenario: Theia attention favicon lifecycle

- **WHEN** tasks need input
- **THEN** every favicon candidate SHALL show the Theia eye with the existing 192 pixel canvas badge geometry, capped at 99
- **WHEN** attention returns to zero or icon loading fails
- **THEN** the original Theia favicon links SHALL be restored, including after delayed image loading
- **AND** notification icon and badge SHALL use /theia-icon-192.png without changing the push worker URL or scope

### Requirement: Account choice before task creation

New task creation SHALL offer backend-compatible accounts when multiple are available, using the project-scoped account roster and authoritative backend kind. It SHALL preselect the user's compatible choice or project default, omit account when no choice is offered, retain selection through failed submission and cancel late reads on close/project change. Task settings SHALL also consume the actual array-shaped accounts response for existing account movement.

#### Scenario: Create on a selected account

- **WHEN** a compatible named account is selected before creation
- **THEN** POST tasks includes that account without an additional account move or session restart
- **AND** changing backend removes incompatible choices

### Requirement: Retained task terminals

The optional workspace SHALL retain at most eight terminal controllers and their scrollback in least recently used order, protecting the selected and split-visible tasks. Task selection SHALL move a retained view without rebuilding its terminal. At the budget limit, the least recently used inactive controller SHALL be destroyed with its listeners, observers, input and output resources. A task disappearing from the snapshot SHALL also dispose its controller. Evicted tasks SHALL rebuild from a fresh bounded snapshot and attach at its byte cursor when revisited. Composer drafts SHALL remain scoped to their task and survive controller eviction. Hidden cached output streams SHALL be suspended to release browser connections; foreground resume SHALL append only unseen bytes from a cursor-fenced catchup snapshot, or replace with bounded replay when the process counter resets or history has been evicted. Pending input SHALL be canceled on task selection independently of the retained output stream. Global disconnect/pagehide SHALL dispose every controller and clear its Fit hint. The unified project grouping and coordinator rosters SHALL include hoisted freelance roles through their owning orchestrator ID.

#### Scenario: Return to a visited task

- **WHEN** the user selects another task and returns before its controller is evicted
- **THEN** the same terminal is shown with deduplicated cursor-based catchup, retaining scrollback when the replay window covers the saved cursor
- **AND** a send interrupted by the selection cannot submit its trailing Enter

#### Scenario: Visit more tasks than the terminal budget

- **WHEN** task navigation exceeds eight cached controllers
- **THEN** the least recently used inactive controller SHALL be destroyed
- **AND** selected and split-visible controllers SHALL remain alive

#### Scenario: Revisit an evicted task

- **WHEN** an evicted task is selected again
- **THEN** a fresh controller SHALL render the latest bounded snapshot exactly once and attach at its byte cursor
- **AND** existing input cancellation and per-task draft rules SHALL remain in force

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

The optional workspace SHALL use one shared delegated tooltip bubble for data-tooltip elements, with a separate aria-label on icon-only controls and no native title on opted-in controls. Hover SHALL wait 400 ms, focus-visible SHALL show immediately, optional data-tooltip-shortcut SHALL render a dim key hint, and Escape, scroll and click SHALL hide it. The bubble SHALL accept pointer events, allow a 200 ms grace to cross the gap, and remain visible while its owner or bubble is hovered or its owner is focused. Leaving both hover and focus SHALL hide it after that grace. The bubble SHALL stay in the viewport, use role=tooltip and preserve existing aria-describedby values. Motion SHALL respect reduced-motion preferences and surfaces SHALL use theme tokens.

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

#### Scenario: Read a tooltip with the pointer
- **WHEN** the pointer crosses from its owner to the tooltip within the close grace
- **THEN** the tooltip SHALL persist while hovered
- **AND** owner focus SHALL retain the tooltip after the pointer leaves
- **AND** Escape SHALL dismiss it without moving focus

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
Attachment sends SHALL write one closed paste, allow output to settle within a bounded wait, and send exactly one standalone CR. Literal image paths SHALL NOT prevent submission. If output never settles, the wait SHALL expire and allow the CR while the captured session remains live. Successful daemon acceptance of CR SHALL clear the captured browser draft and attachments without requiring screen acknowledgment. The client SHALL NOT retry CR or paste automatically. Cancellation and input failures before acceptance SHALL retain the captured draft; disconnect retains its existing clearing behavior. Plain-text sending SHALL retain its existing behavior.

#### Scenario: Slow image ingestion
- **WHEN** an attachment paste finishes asynchronously, either as literal text and paths or an image marker
- **THEN** the client SHALL wait for output settling within a bounded interval before exactly one separate CR
- **AND** it SHALL clear the browser draft and attachments after the daemon accepts CR

#### Scenario: Swallowed Enter
- **WHEN** the daemon accepts CR but the terminal remains unchanged or has an unsupported layout
- **THEN** the browser SHALL clear its accepted draft and attachments
- **AND** the client SHALL NOT send another CR or repeat the paste

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

### Requirement: Settings shortcut transitions
Global shortcuts opening workspace surfaces SHALL leave full-window Settings before opening their target. Settings departure SHALL refuse while a settings save is in flight, matching Back. Creation drafts and compact drawer dismissal SHALL be preserved. Actions that do not change the workspace surface MAY remain in Settings.

#### Scenario: Open another surface from Settings
- **WHEN** Settings Back has focus and a shortcut opens New Task, task settings, a task action dialog, a task detail tab, split view or the task filter
- **THEN** Settings SHALL close and release its inert boundary before the target becomes visible and interactive

#### Scenario: Shortcut during save
- **WHEN** a Settings save is pending and a shortcut would leave Settings
- **THEN** Settings SHALL remain active and no target surface SHALL open
- **AND** the save SHALL finish normally before a later shortcut may leave

#### Scenario: Unsaved edits follow Back behavior
- **WHEN** a user edits Settings without starting a save and invokes a surface shortcut
- **THEN** departure SHALL follow the existing Back behavior without silently saving edits
- **AND** unsaved-edit leave confirmation remains deferred to `workspace-settings-unsaved-edit-confirmation` in the canonical follow-up register above

### Requirement: Accessible asynchronous workspace feedback
The optional workspace SHALL announce asynchronous load failures through alert or live semantics without moving focus. Replacing layout controls SHALL preserve focus on the equivalent control only when that control previously owned focus. Settings SHALL focus Back on initial entry and retain navigation control focus when changing sections.

#### Scenario: Async load fails
- **WHEN** a task tool, review or Settings asynchronous load fails
- **THEN** the error SHALL have alert or live semantics and focus SHALL remain in place

#### Scenario: Layout controls rerender
- **WHEN** a focused Changes tree/flat or Review split/unified control rerenders its content
- **THEN** focus SHALL remain on the equivalent layout control

#### Scenario: Settings section changes
- **WHEN** the focused compact selector or desktop navigation changes section
- **THEN** focus SHALL stay on that navigation control

### Requirement: Review selection memory
The optional workspace SHALL save review path and layout metadata without retaining inactive diff bodies. Only the active rendered review SHALL keep the response text. Selecting another task or review SHALL release the old body while retaining metadata for a fresh read on return.

#### Scenario: Leave a diff review
- **WHEN** another task or review is selected
- **THEN** the old diff body SHALL be released while its path and layout mode remain available for a fresh read on return

### Requirement: Bounded rail refresh work
The optional workspace SHALL skip rail replacement when its rendered inputs are unchanged and SHALL coalesce asynchronous rail updates to at most one replacement per animation frame. Task state and archive input fencing SHALL remain immediate. Ordering, selection, focus and disclosure state SHALL be preserved.

#### Scenario: Unchanged and burst updates
- **WHEN** no-op events or unchanged roster snapshots arrive
- **THEN** the rail SHALL retain its existing nodes
- **AND** a burst of changed events SHALL render the latest state in one animation frame

### Requirement: Visible document polling
Periodic context and fallback task snapshot reads SHALL pause while the optional workspace document is hidden and SHALL refresh once on becoming visible. Context and session usage SHALL refresh on the foreground 15 second cadence. The 30 second task snapshot fallback SHALL run only until task.updated is observed on the current stream and SHALL restart on reconnect. SSE SHALL remain connected. Hidden usage events SHALL invalidate obsolete session state without starting reads, and visibility return SHALL reconcile once.

#### Scenario: Hidden document returns
- **WHEN** a connected workspace remains hidden for sixty seconds
- **THEN** its context and fallback timer polls SHALL make no API reads
- **AND** returning to visibility SHALL refresh each poll source once without reconnecting SSE

### Requirement: Coordinator triage in the unified project rail

Within each project the workspace SHALL show root trees classified active directly below the project without a triage header or divider, using In progress wherever that classification needs a display label. Missing state SHALL default to active. Blocked trees SHALL remain visible and expanded with a secondary Blocked label. Ordinary tasks SHALL precede deferred footers. Nonempty Backlog and Done groups SHALL appear as project footers in that order, collapsed by default, counting root trees rather than descendants. Footer disclosure SHALL persist through guarded browser preferences and remain usable without storage. Nested trees SHALL remain below their parent and pins SHALL sort first within each triage class. Task row geometry, status icons, activity chips, 12px nesting and 220px rail support SHALL remain intact, including reduced motion. Global Active/Archived labels SHALL retain lifetime semantics.

#### Scenario: Grouping and pinned ordering
- **WHEN** a project contains ordinary tasks and root trees in multiple triage states
- **THEN** In progress trees SHALL have no group header and Blocked trees SHALL remain expanded with their classification visible
- **AND** ordinary tasks SHALL remain reachable before nonempty Backlog and Done footers, without a separate team mode
- **AND** pinned roots SHALL remain first within their triage class, including projects containing only Done

#### Scenario: Remembered disclosure and navigation
- **WHEN** a user changes a Backlog or Done disclosure and reloads the workspace
- **THEN** that project and state SHALL retain the preference through guarded storage
- **AND** unavailable or malformed storage SHALL retain usable default disclosures
- **AND** search SHALL temporarily reveal matches without saving that temporary opening, while explicit navigation SHALL reveal the selected tree without changing indentation

#### Scenario: Deferred attention and filtered descendants
- **WHEN** a collapsed footer contains tasks needing input or attention
- **THEN** its visible summary and accessible name SHALL expose those counts and project attention SHALL remain visible
- **WHEN** search finds a descendant whose parent is filtered out
- **THEN** its accessible task name SHALL retain the root tree's triage context

### Requirement: Coordinator menu triage actions

The shared task menu SHALL offer explicit task Pin/Unpin for live lead agents and the four kanban destinations for root trees. Active SHALL display as In progress. Nested trees SHALL NOT offer kanban changes. Ordinary task pins SHALL retain their behavior. Mutations SHALL use existing authenticated REST targets and values, optimistically update the rail, log success and failure, and restore authoritative state with error feedback on failure. Menu keyboard access and focus restoration SHALL remain available when a move hides its source row. Workspace product copy, tooltips and accessible labels SHALL use task, team tasks, agent and unavailable rather than orchestration implementation terms; browser service worker terminology SHALL remain unchanged.

#### Scenario: Coordinator triage mutation
- **WHEN** a root task menu changes triage or pin
- **THEN** the client SHALL POST to the existing orchestrator kanban, pin or unpin endpoint with unchanged values
- **AND** the tree SHALL move or reorder without changing task selection, with Move to In progress as the active destination label

#### Scenario: Failed mutation and focus
- **WHEN** a triage or pin mutation fails
- **THEN** the optimistic change SHALL roll back and existing error feedback SHALL appear
- **AND** focus SHALL remain on the equivalent row action or destination disclosure, returning to the source action if rollback removes the footer

### Requirement: Selected-task staged clipboard
The workspace SHALL show a compact, keyboard-accessible Copy staged text button beside the selected task's terminal only when staging is present, including an empty staged string. Its shared tooltip SHALL show a bounded plaintext preview. Selection SHALL read the existing clipboard endpoint, treating 204 as absent; task output clipboard SSE frames SHALL update or clear staging without changing the terminal output cursor. Reconnecting the output stream SHALL refresh staging. Late reads and events from other selections SHALL NOT replace current state; a live event SHALL supersede an earlier pending read. Malformed frames and staging read failures SHALL be logged without breaking terminal output. Clipboard reads and event logs SHALL NOT include staged text.

#### Scenario: Load and copy repeatedly
- **WHEN** a selected task has staged text
- **THEN** its copy button and preview SHALL appear
- **AND** mouse or keyboard activation SHALL use shared clipboard success/failure feedback
- **AND** repeated copies SHALL preserve staging without a DELETE request
- **AND** Cmd+Shift+C SHALL retain its separate visible-output copy behavior

#### Scenario: Live replacement and clearing
- **WHEN** the selected task receives a clipboard frame with text, including an empty string
- **THEN** the control SHALL copy that exact text
- **WHEN** a cleared frame or a 204 read reports absence
- **THEN** the control SHALL hide and restore composer focus only if it owned focus

#### Scenario: Selection and stream recovery
- **WHEN** selection changes or the task stream reconnects
- **THEN** staging SHALL be read for the selected task
- **AND** late reads, superseded stream events and another task's frames SHALL NOT overwrite selected-task staging

#### Scenario: Staging failure isolation
- **WHEN** a clipboard frame is malformed, a staging read fails or a clipboard write is denied
- **THEN** terminal output SHALL remain usable, failures SHALL be logged without payload text, and copy failures SHALL use shared feedback

### Requirement: Estimated task cost in Info
The optional workspace SHALL show measured Hera role cost only in the selected task's Info tab, formatted as US dollars with two decimals and labeled as an estimate. The client SHALL reuse the existing roster snapshot and refresh without additional fetching or polling. Available role token totals SHALL appear beside cost, including cache read and cache write values carried by REST.

#### Scenario: Worker or freelancer
- **WHEN** the selected task has one live worker or freelance binding with measured cost
- **THEN** Info SHALL show that role's estimated cost and available token breakdown

#### Scenario: Coordinator
- **WHEN** the selected task has a live coordinator binding
- **THEN** Info SHALL show its measured own cost and the measured orchestrator total labeled Team total (this team), identified as an estimate
- **AND** the total SHALL explicitly state that nested teams are excluded
- **AND** a unique coordinator binding SHALL take precedence over a worker binding for the same task

#### Scenario: Missing or ambiguous measurement
- **WHEN** a task is non-Hera, has no measured cost, or has ambiguous bindings without one unique coordinator
- **THEN** Info SHALL omit unavailable cost rows rather than fabricate zero

#### Scenario: Roster refresh
- **WHEN** the existing roster refresh updates cost or bindings
- **THEN** Info SHALL update for the current task without adding cost to the rail or split roster

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

### Requirement: Rail Select mode
A small Select control SHALL sit at the right of the task-count summary and become Cancel while selecting. The header SHALL contain a native tri-state select-all checkbox, an announced selected count, a contextual action and Cancel. Each task row SHALL replace trailing actions with a native named checkbox without widening the row or changing status icons. Project headers SHALL have a tri-state checkbox aligned in that action column. Row body activation SHALL toggle only that task independently of the open viewer. Coordinator checkboxes SHALL cascade through the whole task tree, including collapsed rows; project checkboxes SHALL include every matching task in that project. Parent and select-all checkboxes SHALL be mixed for partial selection. Unchecking a parent SHALL clear its matching subtree. Select-all and cascades SHALL cover only current search matches.

Search SHALL preserve selected IDs and announce N selected, M not shown when search hides selections. Switching Active/Archived, Cancel and Escape SHALL clear selection without applying actions. Disclosures SHALL remain operable and SHALL preserve selections. New arrivals SHALL never become selected automatically, and missing or archived-elsewhere tasks SHALL leave selection. Space SHALL toggle focused native controls; rail Up/Down SHALL move among exposed task checkboxes. Escape SHALL dismiss menus/dialogs before exiting selection. Header Delete SHALL be red in Archived and Archive SHALL remain neutral in Active. The action SHALL be disabled at zero selected.

#### Scenario: Cascade and mixed selection
- **WHEN** a project, coordinator or select-all checkbox is checked
- **THEN** every matching task in its scope SHALL be selected, including collapsed rows, and the confirmation SHALL include every selected task
- **AND** selecting only part of a scope or receiving a new task SHALL expose mixed state without selecting the new task

#### Scenario: Search and cancellation
- **WHEN** search hides selected tasks
- **THEN** selected IDs SHALL be retained and the hidden selected count SHALL be announced
- **AND** Cancel, Escape or a filter switch SHALL exit and clear selection without mutation

### Requirement: Shared task confirmation and bounded execution
Single archive, single restore, single delete, bulk archive and bulk delete SHALL use the same shared confirmation component with task-list body, neutral/danger tone and progress state. Single actions SHALL use the N=1 path. The shared runner SHALL process all task actions with at most four simultaneous per-task requests. Archive SHALL always explicitly POST archive and restore SHALL POST unarchive. Bulk delete SHALL be available only in Archived and MAY include live sessions and live Hera bindings. No typed confirmation SHALL be required.

Large-batch confirmation SHALL have a fixed height, a one-line effects summary, a Needs attention (k) list of tasks flagged by live sessions, named Hera roles/orchestrators or open/draft PR state/link, and Everything else (m) behind a Show list disclosure in capped scroll space. Bulk flags SHALL use only the cached task snapshot, roster and PR state, with no per-task reads. Single delete MAY enrich its own task details with existing cheap REST reads. Bulk delete SHALL use danger styling, title Delete N tasks permanently and button Delete N tasks. Bulk archive SHALL use neutral Archive N tasks. Single actions SHALL retain their established singular labels. Cancel SHALL receive initial focus. Detailed irreversible effects SHALL remain available for inspection. Archive SHALL preserve the open viewer/draft and promptly apply its archived input restrictions.

#### Scenario: Progress and Stop
- **WHEN** a confirmed batch runs
- **THEN** the shared modal SHALL announce completed progress such as 37 of 140 deleted and offer Stop
- **AND** Stop or authentication/connection loss SHALL halt new dispatch while in-flight requests finish
- **AND** outcomes SHALL retain distinct succeeded, failed and not-started task IDs with no automatic delete retries

#### Scenario: Completion and partial failure
- **WHEN** all selected requests succeed
- **THEN** selection SHALL exit with a short notice and focus SHALL reach a surviving row or current filter
- **AND** partial failures SHALL stay selected with an alert naming task failures and their reasons; not-started tasks SHALL remain available for explicit continuation
- **AND** deletion of the open task SHALL select a sensible neighbor without discarding other drafts

### Requirement: Task context categories in the composer
Theia SHALL prefer GET /api/tasks/{id}/context over bridge/meta measurements when the route is supported. It SHALL render measured category token counts for messages, memory, tools, agents, skills, system and compaction. Null categories SHALL be absent, never coerced to zero. Historical role totals SHALL NOT supply categories. A supported response with context=null SHALL show unavailable context, without substituting older bridge/meta data.

#### Scenario: Measured counts and age
- **WHEN** the task context response contains category measurements
- **THEN** the popover shows each measured count, including measured zero, and omits null categories
- **AND** measured_at appears as measurement age and a timestamp
- **AND** live=false is labeled last recorded while live=true remains measured regardless of age

#### Scenario: Old daemon
- **WHEN** the context route returns 404 or HTML through a successful GET fallback
- **THEN** the composer quietly retains its existing bridge/meta context behavior
- **AND** other context read failures clear optional measurements rather than substituting old data

#### Scenario: Context refresh and selection fencing
- **WHEN** the selected task emits task.context_changed on the daemon event stream
- **THEN** Theia refetches its context, also retaining popover-open and foreground 15 second polling refreshes
- **AND** task/session switches and disconnect cancel or ignore superseded reads and clear prior measurements
- **AND** events buffered during a task snapshot also invalidate selected context

### Requirement: Keyboard scrolling of context details
The named context and usage popover SHALL be keyboard focusable and receive focus on opening without trapping Tab navigation. Native keyboard scrolling SHALL reach overflowing content at narrow viewport sizes. Refresh SHALL preserve panel focus and scroll position, and Escape SHALL dismiss the popover and return focus to its trigger.

#### Scenario: Overflow at a narrow viewport
- **WHEN** context categories, quotas and account details overflow at 320 by 600 pixels
- **THEN** keyboard opening focuses the named scroll region
- **AND** PageDown scrolls it and End reaches its lower content
- **AND** refreshing category values retains panel focus and scroll
- **AND** Escape closes the popover and focuses the trigger

### Requirement: Independent coalesced context refresh
Context measurement reads SHALL publish independently of auxiliary account, backend and bridge usage reads. Same-session context events SHALL retain valid displayed measurements while refetching, debounce bursts and share overlapping reads. Context events SHALL NOT refetch auxiliary usage. Auxiliary data SHALL retain its existing foreground cadence and explicit-open refresh. Legacy Hera metadata SHALL be fetched only for unsupported context routes. Task or session replacement SHALL still cancel old reads and clear prior measurements immediately. Supported empty or failed context reads SHALL still clear measurements. Shared reads SHALL have a bounded deadline so a hung request cannot prevent later refreshes.

#### Scenario: Slow accounts
- **WHEN** an account request is still pending and a context result arrives
- **THEN** the context categories publish immediately without awaiting the account read

#### Scenario: Burst and overlapping triggers
- **WHEN** ten same-session context events arrive twenty milliseconds apart
- **THEN** they coalesce into a bounded context-only refresh without blanking displayed categories
- **AND** overlapping open and poll refreshes share in-flight reads

### Requirement: Empty composer suggestion

The workspace SHALL read the selected task's suggestion from GET /api/tasks/{id}/bridge and display it as a plain-text textarea placeholder only while the draft is empty and the bridge is live authority for that session and generation. Suggestions SHALL never replace typed or restored drafts, fill the draft automatically or send input. Unsupported providers and unavailable bridge state SHALL retain the normal empty hint.

#### Scenario: Suggestion and draft preservation
- **WHEN** a live suggestion arrives while a draft is empty or contains typed or restored text
- **THEN** only the empty draft shows the suggestion, the draft value remains unchanged and nothing is sent

#### Scenario: Suggestion invalidation
- **WHEN** the provider clears the suggestion, a new turn starts, the session changes or bridge authority expires or becomes unavailable
- **THEN** the suggestion placeholder clears and the normal empty hint returns

#### Scenario: Refresh from events
- **WHEN** task.suggestion_changed arrives for the selected task
- **THEN** the client refreshes GET /bridge without reading suggestion text from the event or terminal

#### Scenario: Selection and disconnect fencing
- **WHEN** task selection, disconnect or a newer refresh supersedes an in-flight bridge read
- **THEN** the prior hint clears synchronously, the request is canceled and late results cannot populate the current composer

#### Scenario: Bounded authority checks and accessibility
- **WHEN** a suggestion is visible in an empty draft and the page is visible
- **THEN** authority checks run at approximately 15-second intervals with one request in flight and a bounded read deadline
- **AND** no polling runs without a visible suggestion, event bursts are debounced, the existing textarea label remains and no aria-live region is added

### Requirement: Web terminal keeps the user's place and draft across a resize kick

The web terminal SHALL report `scrolled_up` (the xterm viewport is above the bottom) and `composer_dirty` (a typed-since-submit fold over bytes sent to the agent: printable and pasted content set it; Enter, Ctrl+C and Ctrl+U clear it; escape sequences, other control bytes and DEL are ignored) on every resize POST. When the response carries `deferred`, the client SHALL re-POST the current size when the viewport returns to the bottom and every 10 seconds, and SHALL stop when the deferral clears, a kick lands, the selected task changes, the page is hidden or the terminal view is torn down. The shell asset change SHALL bump the service worker version.

#### Scenario: Reading history during a resize

- **WHEN** the user is scrolled up and the pane width changes past the margin
- **THEN** the session is not restarted until the viewport is back at the bottom

#### Scenario: Typed but unsent text

- **WHEN** the user has typed in the terminal and not pressed Enter
- **THEN** the resize is deferred and retried until the text is submitted or cleared

#### Scenario: View goes away

- **WHEN** the user switches task or the page is hidden while a deferral is pending
- **THEN** the retry timer is cancelled and no further resize is posted

