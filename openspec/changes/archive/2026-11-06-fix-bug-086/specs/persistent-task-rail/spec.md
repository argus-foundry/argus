## MODIFIED Requirements

### Requirement: Browsing binds without starting sessions

Moving the rail cursor to a task SHALL display its existing session or recorded output without starting or restarting it. Enter on a task SHALL focus its terminal using the existing task start/resume and Hera close-out guards. Completed and archived tasks SHALL be viewable without implicit restart. Rail navigation and every cursor-restore path (startup, refresh, fold/expand, F, filters, deletion/archive and rail enable) SHALL select only task rows, skipping project and section headers. While any task row is visible, the selection hint SHALL NOT appear. Removing the selected task or hiding all tasks SHALL clear stale terminal bindings and follow another visible task or display an empty-state hint when no task is visible. Periodic refreshes of an unchanged selection/session SHALL NOT reset its terminal.

#### Scenario: Browse a stopped task

- **WHEN** the cursor moves to a stopped task
- **THEN** its recording or placeholder is shown and no session starts

#### Scenario: Explicit entry retains lifecycle safeguards

- **WHEN** Enter is pressed on an eligible stopped task or a task awaiting Hera close-out
- **THEN** the existing start/resume or close-out checks govern entry, and merely browsing never bypasses those checks

#### Scenario: Selection disappears

- **WHEN** the selected task is deleted or excluded by a filter
- **THEN** the pane follows the new visible task selection or clears to a hint when no task is visible, and stale output/input bindings cannot remain active

#### Scenario: Navigate across headers

- **WHEN** j, k, Up or Down crosses a project or section boundary or reaches either end of the rail
- **THEN** the cursor skips all headers and stays on a task, expanding the destination project as needed

#### Scenario: Restore a task selection

- **WHEN** startup restore, refresh, fold/expand, F, filtering, deletion, archive or enabling the rail rebuilds the rows
- **THEN** a visible task is selected whenever any task row is visible, and the content pane never displays the selection hint alongside visible tasks
