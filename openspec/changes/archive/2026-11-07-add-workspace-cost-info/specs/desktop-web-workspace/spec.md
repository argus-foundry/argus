## ADDED Requirements

### Requirement: Estimated task cost in Info
The optional workspace SHALL show measured Hera role cost only in the selected task's Info tab, formatted as US dollars with two decimals and labeled as an estimate. The client SHALL reuse the existing roster snapshot and refresh without additional fetching or polling. Available role token totals SHALL appear beside cost, including cache read and cache write values carried by REST.

#### Scenario: Worker or freelancer
- **WHEN** the selected task has one live worker or freelance binding with measured cost
- **THEN** Info SHALL show that role's estimated cost and available token breakdown

#### Scenario: Coordinator
- **WHEN** the selected task has a live coordinator binding
- **THEN** Info SHALL show its measured own cost and the measured orchestrator total labeled Team total (this coordinator), identified as an estimate
- **AND** the total SHALL not claim to include nested teams
- **AND** a unique coordinator binding SHALL take precedence over a worker binding for the same task

#### Scenario: Missing or ambiguous measurement
- **WHEN** a task is non-Hera, has no measured cost, or has ambiguous bindings without one unique coordinator
- **THEN** Info SHALL omit unavailable cost rows rather than fabricate zero

#### Scenario: Roster refresh
- **WHEN** the existing roster refresh updates cost or bindings
- **THEN** Info SHALL update for the current task without adding cost to the rail or split roster
