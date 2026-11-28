## ADDED Requirements

### Requirement: Closed board feedback result context

After receipt replay and existing payload validation, a new feedback action rejected because its owning board item is already resolved SHALL retain outcome `invalid` and include the current resolved item in `item`. Matching receipt replay SHALL still return its original successful result. Changed-payload action ID reuse SHALL remain invalid without closed-item context. Other items in the same request SHALL be judged independently.

#### Scenario: Closed comment beside fresh feedback

- **WHEN** a valid comment targets an item closed since page load and another comment targets an unchanged open item
- **THEN** the closed comment returns invalid with its resolved item and the open comment is committed and returns commented

#### Scenario: Replay before closure

- **WHEN** an accepted comment is replayed with its identical action ID and payload after the item closes
- **THEN** its original commented receipt is returned without another notice
