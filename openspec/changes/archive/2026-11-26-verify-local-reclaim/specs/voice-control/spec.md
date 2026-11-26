## ADDED Requirements

### Requirement: Reconcile an authoritative task deletion

A successful PWA delete SHALL close the currently displayed detail if it shows the deleted task, including a reopened visit. A different task's detail SHALL remain open.

#### Scenario: Reopening a task during Delete
- **WHEN** a delete finishes while the reopened detail still shows that deleted task
- **THEN** the detail closes. If the selected task has a different identity, its terminal stays open.
