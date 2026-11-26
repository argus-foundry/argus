## ADDED Requirements

### Requirement: Delayed detail actions preserve the current visit

A response to a PWA task mutation SHALL update or replace a detail view only if that view is still the one that initiated the mutation.

#### Scenario: A fork returns after navigating elsewhere
- **WHEN** the user leaves a detail view before its fork request or subsequent task refresh completes
- **THEN** the fork does not replace the detail view the user chose.
