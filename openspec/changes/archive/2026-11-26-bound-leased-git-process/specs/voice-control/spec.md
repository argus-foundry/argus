## ADDED Requirements

### Requirement: Reconcile authoritative task archival

After a successful Archive or Unarchive, the PWA SHALL close the detail for that task if it no longer belongs in the active list filter. A different task's detail SHALL remain open.

#### Scenario: An archive response returns after the same task is reopened
- **WHEN** success removes the task from the selected Active or Archived list
- **THEN** its reopened terminal closes, while an unrelated task's terminal stays open.
