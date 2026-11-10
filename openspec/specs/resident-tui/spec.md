# resident-tui Specification

## Purpose
TBD - created by archiving change add-resident-agent. Update Purpose after archive.
## Requirements
### Requirement: The resident tab is first and named after the agent

When the resident is enabled the TUI header SHALL show the resident tab first, labelled with `resident.name`, for example `Solo | Tasks | Projects | Settings`. `1`, `2`, `3` SHALL continue to select Tasks, Projects and Settings; a new rebindable global action `global.tab_resident` (default `0`) SHALL select the resident tab. The tab SHALL be the Hera view with exactly one coordinator, the resident: a rail of the resident's workers, the resident's conversation pane bound to the resident session with input going straight to the PTY, and the resident's inbox. Selecting the resident row SHALL show the resident pane; selecting a worker row SHALL show that worker in the existing worker pane. The inbox SHALL be the existing read-only inbox viewer opened with `i`, and the rail SHALL show the unread count and the existing needs-input and ready-to-close markers per worker so a stuck agent is visible. The rail SHALL let the human manage workers but never create them: the existing nuke, hide, rename and role-status actions SHALL act on worker rows (and SHALL be inert on the resident's own row), while every creation or structure-changing action (new coordinator, spawn, move, join, detach, pin, kanban step, bounce) SHALL be inert in this tab and absent from its `?` overlay and ctrl+k palette. When the human nukes, hides or renames a worker the resident SHALL be notified (see the `resident-coordinator` capability). The resident tab SHALL NOT be restored by last-tab persistence; a launch whose persisted tab would be the resident SHALL land on Tasks. When the resident is disabled the tab and its marker SHALL NOT appear.

#### Scenario: Tab order and digits

- **WHEN** the resident is enabled and named "Solo"
- **THEN** the header reads `Solo | Tasks | Projects | Settings` and `1/2/3` still select Tasks/Projects/Settings

#### Scenario: Disabled hides everything

- **WHEN** the resident is disabled
- **THEN** no resident tab, marker or rail box is shown

#### Scenario: Not restored on launch

- **WHEN** the TUI exits on the resident tab and is relaunched
- **THEN** it lands on the Tasks tab

#### Scenario: Opens on the resident conversation

- **WHEN** the user opens the resident tab
- **THEN** keyboard focus is on the resident conversation pane (input goes straight to its PTY), `ctrl+alt+←` reaches the team rail, and `ctrl+q` returns to the previous screen from either

#### Scenario: Unread counts

- **WHEN** a worker (or the resident) has unread Hera or task mail
- **THEN** the worker's rail row shows `✉N` and the resident pane title shows the resident's own unread count

#### Scenario: Rail lists the team

- **WHEN** the resident has three workers, one flagged needs-input
- **THEN** the rail shows the three workers, the flagged one with its needs-input marker, beside the resident pane

#### Scenario: Worker selection

- **WHEN** the user selects a worker row
- **THEN** that worker's pane or details are shown and the resident session is unaffected

#### Scenario: Inbox viewer

- **WHEN** the user presses `i` on the resident tab
- **THEN** the read-only inbox viewer opens and does not acknowledge any message

#### Scenario: No creation actions

- **WHEN** the user presses any spawn, new-coordinator, move, join, detach, pin, kanban or bounce key on the resident rail
- **THEN** nothing happens and none of them is listed in the help overlay or palette for this tab

#### Scenario: Worker management works

- **WHEN** the user presses nuke, hide, rename or the role-status key with a worker row selected
- **THEN** the existing action runs on that worker and is listed in the help overlay for this tab

#### Scenario: Resident row is protected

- **WHEN** the user presses nuke, hide, rename or the role-status key with the resident row selected
- **THEN** nothing happens

#### Scenario: Resident pane lifecycle unchanged

- **WHEN** a live session is newly bound to the resident pane
- **THEN** the existing first-attach resync applies and the Hera size-drift kick is not armed

### Requirement: A global hotkey toggles the resident and returns exactly

The `global.resident` action (default `ctrl+\`) SHALL be dispatched unconditionally from `CtxGlobal` like `ctrl+k` and `ctrl+j`, except while a modal or text-input mode is open, where it SHALL be ignored. From any non-resident screen it SHALL record a `residentReturn` snapshot of the current tab, mode, agent-view task and zoom, Hera selection, focus and fullscreen state (through the existing Hera excursion snapshot) and Settings category and focus, then open the resident tab. On the resident tab, `global.resident` and `ctrl+q` SHALL restore the snapshot exactly and SHALL return DIRECTLY to the origin tab and mode with a single tab switch: no intermediate switch to Tasks, and no extra last-tab persistence or rail-box sync. A pane that is no longer present in the Hera layout (a focus-only mismatch) SHALL NOT count as a vanished target: the tab, mode and selection are restored and focus is applied best-effort. If no snapshot exists or its target vanished (for example the selected task or role no longer exists), they SHALL land on the Tasks list. Any other navigation away from the resident tab SHALL discard the snapshot.

#### Scenario: Round trip from a zoomed agent view

- **WHEN** the user is in a zoomed agent view, presses the hotkey, then presses it again
- **THEN** the same task's agent view returns with the same zoom and focused pane

#### Scenario: Round trip from the Hera tab

- **WHEN** the user is on the Hera tab with a coordinator pane focused, toggles to the resident and back
- **THEN** the same Hera selection and focused pane are restored

#### Scenario: Ignored in a modal

- **WHEN** a modal or text-input mode is open and the hotkey is pressed
- **THEN** the key is not consumed as a toggle and the modal is unaffected

#### Scenario: Direct return without a Tasks detour

- **WHEN** the user toggles back from the resident tab to the Tasks list, an agent view, the Hera tab or Settings
- **THEN** exactly one tab switch to the origin is performed and the persisted last tab is the origin tab

#### Scenario: Hera focus mismatch

- **WHEN** the recorded Hera pane is no longer present when the user returns
- **THEN** the Hera tab and selection are restored with best-effort focus and the TUI does not fall back to Tasks

#### Scenario: Vanished target

- **WHEN** the recorded task no longer exists when the user returns
- **THEN** the TUI lands on the Tasks list

#### Scenario: Rebinding

- **WHEN** the user rebinds `global.resident` in `[keybindings]`
- **THEN** the new key toggles the resident and the `?` help overlay shows it

### Requirement: ctrl+g on the resident tab stays scoped to the team

On the resident tab, the global jump-to-next-needs-input key SHALL jump within the standing team when a team worker needs input, staying on the resident tab without arming the Projects rail excursion. When no team worker needs input but a Projects role does, it SHALL switch to the Projects tab, arm the excursion on the Projects rail's own state from before the jump, and jump to that role, so the excursion-restore key returns the Projects rail to that state. When nothing needs input anywhere it SHALL only show the "No role needs input" notice and SHALL NOT switch tabs or change the resident tab's focus, selection, return snapshot or any held Projects excursion.

#### Scenario: Team worker needs input

- **WHEN** the resident tab is showing and a resident worker needs input
- **THEN** that worker is selected on the resident tab and no Projects excursion is armed

#### Scenario: Only a Projects role needs input

- **WHEN** the resident tab is showing and only a Projects role needs input
- **THEN** the Projects tab shows with that role selected, and the restore key returns the Projects rail to its prior selection

#### Scenario: Nothing needs input

- **WHEN** the resident tab is showing and nothing needs input anywhere
- **THEN** only the notice appears; the tab, focus, selected row, return snapshot and any held Projects excursion are unchanged

### Requirement: The resident pane resizes on first attach and explains why it is not running

When a live session is newly bound to the resident pane, the TUI SHALL schedule a one-shot forced PTY resync so the session is resized to the pane even when no width delta would be detected. The resident pane SHALL NOT use the Hera size-drift kill-and-resume kick machinery: that machinery is coupled to Hera pane bindings and its restart path would race the daemon's ensure loop. The TUI SHALL carry `ResidentStateResp.Reason` from its existing poll (UI goroutine only, control bytes stripped) into the resident tab's no-session placeholder and into the Settings -> Resident detail pane (`Not running: <reason>`), and SHALL clear it when the daemon reports none. Once the resident has run, its stopped tab replays the dead session's log instead of drawing the placeholder, so while the resident is not running and a reason exists the pane SHALL also draw the reason as a one-line banner on its first inner row over the replay (UI goroutine only, truncated to the pane width, no screen sync); the banner SHALL be absent while a live session is attached and once the reason is empty. The TUI SHALL publish a dedicated resident size in its initial-PTY layout report, equal to the full-width resident pane's inner rect (the host screen minus the header row, the status bar row and the pane's one-cell borders), independent of whichever view is active.

#### Scenario: Replacement session is resized

- **WHEN** a new live session is bound to the resident pane at an unchanged pane size
- **THEN** the session receives a resize to the pane size

#### Scenario: Reason visible

- **WHEN** the daemon reports a reason such as "waiting for the Argus MCP server" and no session is attached
- **THEN** the resident tab placeholder and Settings -> Resident both show it, and both clear when the reason is empty

#### Scenario: Reason visible over a replayed pane

- **WHEN** the resident has run before, so its stopped tab replays the dead session's log, and the daemon reports a reason
- **THEN** a one-line banner with the reason is drawn over the replay, and it disappears when the resident runs or the reason clears

#### Scenario: Resident size is its own pane

- **WHEN** the user is on a view narrower than the resident pane (for example a Hera worker pane)
- **THEN** the published resident size is still the full-width resident pane's inner size

### Requirement: Rail box shows the resident's attention state beside needs-attention

A `ResidentBox` SHALL be stacked with the existing attention box in the agent-view left column, the Hera rail summary area and the Tasks rail. It SHALL have height 0 when the resident is neither unread nor waiting; otherwise exactly one row showing the agent name, `●` for unread or `?` for waiting (waiting wins), and as much of the stored one-line preview as fits, truncated with `…`. Changing its height SHALL resize only the left column through the existing height-change flow and SHALL NOT resize or re-kick any PTY pane. It SHALL be hidden while the resident tab itself is showing.

#### Scenario: Unread reply appears beside needs-attention

- **WHEN** the resident replies while the user is in an agent view and two agents also need attention
- **THEN** the left column shows both the needs-attention box with two entries and the resident box with `●` and the preview

#### Scenario: Waiting outranks unread

- **WHEN** the resident has an unread reply and a pending confirmation
- **THEN** the box shows `?`

#### Scenario: No PTY resize

- **WHEN** the box appears or disappears
- **THEN** no agent pane receives a resize and no size-drift kick is armed

#### Scenario: Zero height when quiet

- **WHEN** the resident is read and not waiting
- **THEN** the box occupies no rows

### Requirement: The tab label carries the marker on every screen

The resident tab label in the header SHALL show `●` when unread and `?` when waiting, on every screen including screens with no left column such as the plain Tasks list and Settings. The marker SHALL clear when the resident pane is shown and focused.

#### Scenario: Marker on a rail-less screen

- **WHEN** the resident replies while the user is on Settings
- **THEN** the header shows `Solo ●` and no other element changes

#### Scenario: Clears on view

- **WHEN** the user opens the resident tab
- **THEN** the marker clears

### Requirement: Settings has a Resident category

Settings SHALL provide a Resident category with enabled, name, context budget, memory directory, model and account, plus a read-only pointer to `ask_tools`/`deny_tools` in `config.toml`. Edits SHALL be local-only like Accounts, require a valid name before enabling (the field starts empty, with no default), and show the missing KB prerequisite when enabling is not possible.

#### Scenario: Rename takes effect

- **WHEN** the user changes the name to "Ari" and saves
- **THEN** the tab label, rail box and marker use "Ari" without restarting the session

#### Scenario: Enable blocked until named

- **WHEN** the user turns on enabled while the name is empty
- **THEN** the category asks for a name, the resident does not start, and nothing is persisted as enabled

#### Scenario: Enable blocked by prerequisite

- **WHEN** the user enables the resident with the KB disabled
- **THEN** the category shows the missing prerequisite and the resident does not start

