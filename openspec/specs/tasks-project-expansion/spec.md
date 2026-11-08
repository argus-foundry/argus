# Tasks project expansion

## Purpose

Let users keep every Tasks project expanded, or return to focus-fold navigation, with a remembered mode shared by the classic list and optional Tasks-tab rail.

## Requirements

### Requirement: Remembered Tasks project expansion mode

Both classic Tasks and the Tasks-tab rail SHALL offer rebindable `tasklist.expand_all`, default `F`, toggling all-projects-expanded and focus-fold. The mode SHALL persist locally as `ui.tasks_expand_all`, default false; remote mode SHALL retain the choice only for that process. Filter-input F and terminal-focused F SHALL remain input. Help and README Reference SHALL document the action.

In all-open mode every admitted active project SHALL stay expanded through navigation and refresh until toggled off. Manual folder actions SHALL NOT close projects in this mode. Toggling off SHALL keep the selected task and show only its active project expanded (or the last focused active project if selection is elsewhere). This two-mode policy SHALL supersede independent project folds; focus-fold SHALL be the default in both layouts. Rail headers SHALL NOT be selectable or manually foldable; navigation SHALL expand the destination project and select its task directly.

Pinned SHALL remain expanded. Classic Archive SHALL retain its existing outer fold behavior. Rail Archive SHALL open when navigation enters it, remain open on navigation away, and open when only archived tasks remain. When open, all-open SHALL show all archived projects, while focus-fold SHALL show only the focused archived project. Rebuilds SHALL replace a missing expansion target with an admitted project so task rows remain reachable.

#### Scenario: Keep all projects open

- **WHEN** F enables all-open in either Tasks layout and the user navigates through projects
- **THEN** every active project's rows remain visible until F is pressed again

#### Scenario: Return to focus-fold

- **WHEN** F disables all-open with a task in project B selected
- **THEN** the same task remains selected, B stays open, and other active projects close

#### Scenario: Remember across relaunch

- **WHEN** all-open is enabled locally and Argus relaunches
- **THEN** both Tasks layouts restore all-open; without a saved value, focus-fold is used

#### Scenario: Preserve archive and filters

- **WHEN** all-open is enabled with Archive closed and H or a substring filter excludes tasks
- **THEN** Archive remains closed unless filter behavior expands it or only archived tasks remain in the rail, and excluded tasks remain excluded

#### Scenario: Preserve filter and terminal input

- **WHEN** F is typed in a terminal/filter or the user enters a focused agent
- **THEN** the key remains ordinary input and does not change expansion

#### Scenario: Navigate into rail Archive

- **WHEN** task navigation crosses from active tasks into a closed Archive
- **THEN** Archive opens and its first task is selected without stopping on a header, and remains open when navigation leaves it

#### Scenario: Remove the expanded project

- **WHEN** the expanded project's last task is deleted, archived or excluded
- **THEN** another admitted project is expanded and the rail selects a task whenever any admitted tasks remain
