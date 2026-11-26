## ADDED Requirements

### Requirement: Interpret omitted task archive state

The PWA SHALL treat an omitted archived field in an authoritative task response as false.

#### Scenario: The API omits archived after Unarchive
- **WHEN** an older Archive response checks the reopened task and the GET response omits archived
- **THEN** the Active view stays open.
