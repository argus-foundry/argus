## ADDED Requirements

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
