## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Conductor composer layout
The optional workspace SHALL show a large rounded draft box with no default placeholder text, except a verified provider suggestion while the draft is empty under Empty composer suggestion. Backend/model identity SHALL be read-only; context, attach and send controls SHALL be aligned on the right. A file picker SHALL reuse the existing upload path and attachment constraints. Attachment instructions SHALL appear only for errors, while chips and existing paste/drop behavior remain.

#### Scenario: Native composer actions
- **WHEN** a task is selected and files are chosen with the attach button
- **THEN** uploads belong to that task and no terminal input is sent until explicit send
- **AND** native paste, dictation, IME and per-task drafts preserve existing behavior
