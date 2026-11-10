## ADDED Requirements

### Requirement: Subscribers learn presence from the operation

Clipboard store subscribers SHALL be told whether a value is present by the operation: `Set`, including with empty text, is present; `Clear` and expiry are not.

#### Scenario: Empty set is present

- **WHEN** `Set("")` is called for a task
- **THEN** subscribers receive a present update with empty text
