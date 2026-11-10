## ADDED Requirements

### Requirement: The resident uses its own budget through the single recycle path

The resident's context budget SHALL be `resident.context_budget`; the coordinator budget SHALL NOT apply to it even though it holds a coordinator role. Crossing the budget SHALL nudge the resident to flush its open loops and recycle through the resident recycle runner, and SHALL NOT set the generic coordinator pending-recycle flag. The resident's `context_size` stamp SHALL continue to feed the Hera view's context indicator.

#### Scenario: Budget source

- **WHEN** the coordinator budget is 300000 and `resident.context_budget` is 150000
- **THEN** the resident is nudged at 150000

#### Scenario: No second flag

- **WHEN** the resident crosses its budget
- **THEN** the nudge is delivered and the generic coordinator recycle-request flag is not set
