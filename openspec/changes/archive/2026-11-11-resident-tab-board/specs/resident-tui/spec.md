## MODIFIED Requirements

### Requirement: The resident tab is first and named after the agent

When the resident is enabled the TUI header SHALL show the resident tab first, labelled with `resident.name`, for example `Solo | Tasks | Projects | Settings`. `1`, `2`, `3` SHALL continue to select Tasks, Projects and Settings; a new rebindable global action `global.tab_resident` (default `0`) SHALL select the resident tab. The tab SHALL be the Hera view with exactly one coordinator, the resident: a rail of the resident's workers, the resident's conversation pane bound to the resident session with input going straight to the PTY, and the resident's inbox. Selecting the resident row SHALL show the resident pane; selecting a worker row SHALL show that worker in the existing worker pane. The inbox SHALL be the existing read-only inbox viewer opened with `i`, and the rail SHALL show the unread count and the existing needs-input and ready-to-close markers per worker so a stuck agent is visible. The rail SHALL let the human manage workers but never create them: the existing nuke, hide, rename and role-status actions SHALL act on worker rows (and SHALL be inert on the resident's own row), while every creation or structure-changing action (new coordinator, spawn, move, join, detach, pin, kanban step, bounce) SHALL be inert in this tab and absent from its `?` overlay and ctrl+k palette. When the human nukes, hides or renames a worker the resident SHALL be notified (see the `resident-coordinator` capability). The resident tab SHALL NOT be restored by last-tab persistence; a launch whose persisted tab would be the resident SHALL land on Tasks. When the resident is disabled the tab and its marker SHALL NOT appear.

The resident tab's own coordinator rail row SHALL display `resident.name`, validated with the same name rule as the tab label, falling back to `resident` if empty or invalid. It SHALL follow Settings renames live without restarting or reattaching the session. This display-only substitution SHALL NOT change orchestrator or role identities in DB, MCP, REST or worker prompts, worker row labels, Projects rail labels or jump-switcher labels.

The resident tab SHALL expose the standing coordinator's human board in Details using the same controls as Projects: auto-show new unread items when coordinator-board auto-show is enabled, show the unread count in the title, `p` toggle plan/board from the rail or Details, `j`/`k` move, `r` compose a reply into the resident conversation without submitting, and `x` resolve the selected item as the human. Board keys SHALL appear in resident help and applicable palette entries without relaxing any existing rail restrictions.

#### Scenario: Resident rail name follows a rename

- **WHEN** the resident is named "Solo" and the human renames it to "Ari" in Settings → Resident
- **THEN** its resident-tab rail row initially shows "Solo" and updates to "Ari" live without a session restart, while worker labels and stored Hera names remain unchanged

#### Scenario: Invalid resident rail name falls back

- **WHEN** the resident rail is rendered with an empty or invalid configured name
- **THEN** its coordinator row shows `resident`

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
- **THEN** keyboard focus is on the resident conversation pane (input goes straight to its PTY), except directional entry from Tasks focuses the rightmost present pane, `ctrl+alt+←` reaches the team rail, and `ctrl+q` returns to the previous screen from either

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

#### Scenario: Optional spatial navigation

- **WHEN** cross-tab arrows are enabled in local mode
- **THEN** the resident rail, conversation and worker panes precede Tasks in the continuous chain, with modified arrows intercepted before PTY input and hidden panes skipped; modal, filter and fullscreen guards still apply

#### Scenario: Posted reminder is visible

- **WHEN** the resident posts an unread reminder and coordinator-board auto-show is enabled
- **THEN** the resident tab Details auto-shows the item, including arrivals while the tab is already open, and its title counts the unread item

#### Scenario: Board toggle

- **WHEN** the human presses `p` from the resident rail or Details
- **THEN** Details toggles between board and plan; refreshing the same unread items preserves a deliberate toggle

#### Scenario: Board navigation and reply

- **WHEN** the human navigates the resident board with `j`/`k` and presses `r`
- **THEN** the selected item's reply prefix is pasted into the resident conversation without submission and that conversation receives focus

#### Scenario: Human dismissal

- **WHEN** the human presses `x` on a resident board item
- **THEN** the item is resolved as the human and disappears from the open board

#### Scenario: Board keys discoverable

- **WHEN** the human opens resident help or the applicable board command palette
- **THEN** the live board bindings are listed and the existing creation and management restrictions remain unchanged

### Requirement: Rail box shows the resident's attention state beside needs-attention

A `ResidentBox` SHALL be stacked with the existing attention box in the agent-view left column, the Hera rail summary area and the Tasks rail. It SHALL have height 0 when the resident is neither unread (reply or board item) nor waiting; otherwise exactly one row showing the agent name, `●` for an unread reply or board item or `?` for waiting (waiting wins), and as much of the stored one-line preview as fits, truncated with `…`. Changing its height SHALL resize only the left column through the existing height-change flow and SHALL NOT resize or re-kick any PTY pane. It SHALL be hidden while the resident tab itself is showing.

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

#### Scenario: Unread board item raises the rail box

- **WHEN** the resident has an unread board item and no unread reply while the human is outside the resident tab
- **THEN** the resident box shows `●`; reading or dismissing the board item clears its contribution to attention

### Requirement: The tab label carries the marker on every screen

The resident tab label in the header SHALL show `●` when a reply or board item is unread and `?` when waiting, on every screen including screens with no left column such as the plain Tasks list and Settings. The marker SHALL be suppressed while the resident pane is shown and focused. Viewing the conversation SHALL acknowledge unread replies but SHALL NOT mark board items read; their unread attention SHALL return on leaving until they are read or dismissed.

#### Scenario: Marker on a rail-less screen

- **WHEN** the resident replies while the user is on Settings
- **THEN** the header shows `Solo ●` and no other element changes

#### Scenario: Clears on view

- **WHEN** the user opens the resident tab
- **THEN** the marker clears

#### Scenario: Unread board item lights the resident marker

- **WHEN** the resident posts an unread board item while the human is on another tab and there is no unread reply
- **THEN** the header shows `●`, opening the resident tab suppresses it without acknowledging the board item, and leaving restores it until the item is read or dismissed
