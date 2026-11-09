# resident-tui Specification

## Purpose
TBD - created by archiving change add-resident-agent. Update Purpose after archive.
## Requirements
### Requirement: The resident tab is first and named after the agent

When the resident is enabled the TUI header SHALL show the resident tab first, labelled with `resident.name`, for example `Solo | Tasks | Projects | Settings`. `1`, `2`, `3` SHALL continue to select Tasks, Projects and Settings; a new rebindable global action `global.tab_resident` (default `0`) SHALL select the resident tab. The tab SHALL show one full-width terminal pane bound to the resident session, with input going straight to the PTY. The resident tab SHALL NOT be restored by last-tab persistence; a launch whose persisted tab would be the resident SHALL land on Tasks. When the resident is disabled the tab and its marker SHALL NOT appear.

#### Scenario: Tab order and digits

- **WHEN** the resident is enabled and named "Solo"
- **THEN** the header reads `Solo | Tasks | Projects | Settings` and `1/2/3` still select Tasks/Projects/Settings

#### Scenario: Disabled hides everything

- **WHEN** the resident is disabled
- **THEN** no resident tab, marker or rail box is shown

#### Scenario: Not restored on launch

- **WHEN** the TUI exits on the resident tab and is relaunched
- **THEN** it lands on the Tasks tab

### Requirement: A global hotkey toggles the resident and returns exactly

The `global.resident` action (default `ctrl+\`) SHALL be dispatched unconditionally from `CtxGlobal` like `ctrl+k` and `ctrl+j`, except while a modal or text-input mode is open, where it SHALL be ignored. From any non-resident screen it SHALL record a `residentReturn` snapshot of the current tab, mode, agent-view task and zoom, Hera selection, focus and fullscreen state (through the existing Hera excursion snapshot) and Settings category and focus, then open the resident tab. On the resident tab, `global.resident` and `ctrl+q` SHALL restore the snapshot exactly. If no snapshot exists or its target vanished, they SHALL land on the Tasks list. Any other navigation away from the resident tab SHALL discard the snapshot.

#### Scenario: Round trip from a zoomed agent view

- **WHEN** the user is in a zoomed agent view, presses the hotkey, then presses it again
- **THEN** the same task's agent view returns with the same zoom and focused pane

#### Scenario: Round trip from the Hera tab

- **WHEN** the user is on the Hera tab with a coordinator pane focused, toggles to the resident and back
- **THEN** the same Hera selection and focused pane are restored

#### Scenario: Ignored in a modal

- **WHEN** a modal or text-input mode is open and the hotkey is pressed
- **THEN** the key is not consumed as a toggle and the modal is unaffected

#### Scenario: Vanished target

- **WHEN** the recorded task no longer exists when the user returns
- **THEN** the TUI lands on the Tasks list

#### Scenario: Rebinding

- **WHEN** the user rebinds `global.resident` in `[keybindings]`
- **THEN** the new key toggles the resident and the `?` help overlay shows it

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

