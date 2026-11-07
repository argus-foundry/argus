## ADDED Requirements

### Requirement: Bounded rail refresh work
The optional workspace SHALL skip rail replacement when its rendered inputs are unchanged and SHALL coalesce asynchronous rail updates to at most one replacement per animation frame. Task state and archive input fencing SHALL remain immediate. Ordering, selection, focus and disclosure state SHALL be preserved.

#### Scenario: Unchanged and burst updates
- **WHEN** no-op events or unchanged roster snapshots arrive
- **THEN** the rail SHALL retain its existing nodes
- **AND** a burst of changed events SHALL render the latest state in one animation frame

### Requirement: Visible document polling
Periodic context and fallback task snapshot reads SHALL pause while the optional workspace document is hidden and SHALL refresh once on becoming visible. Foreground cadences SHALL remain unchanged and SSE SHALL remain connected.

#### Scenario: Hidden document returns
- **WHEN** a connected workspace remains hidden for sixty seconds
- **THEN** its context and fallback timer polls SHALL make no API reads
- **AND** returning to visibility SHALL refresh each poll source once without reconnecting SSE
