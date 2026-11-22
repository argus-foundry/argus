## ADDED Requirements

### Requirement: Accurate partial response feedback

Decide SHALL explain a result containing an already-resolved item as closed feedback that was not saved rather than bare invalid. It SHALL display a missing/oversized note warning only when the note is actually required and empty or exceeds its byte limit. A conflict that clears the pending response SHALL preserve the note and show the item-changed state without falsely claiming the note is missing. Unchanged items SHALL clear on confirmed success despite sibling conflicts, and re-answering conflicted items SHALL submit the new revision with a new action ID.

#### Scenario: Two coordinators with mixed revisions

- **WHEN** four responses are submitted across two coordinators and two items changed during review
- **THEN** unchanged responses are accepted, only the changed items show the stale state, and re-answering those two items succeeds without resending the accepted responses

#### Scenario: Closed FYI comment

- **WHEN** an FYI comment result identifies its item as resolved
- **THEN** the page explains that the item is already resolved and the comment was not saved, retaining the note for recovery
