## ADDED Requirements

### Requirement: Follow-up timer discipline

Resident orientation SHALL tell Solo to set a purposeful follow-up when promising to check back or waiting on someone, record timer ids with its open loops, reconcile active timers on startup, and cancel timers when their loop closes. It SHALL distinguish resident wake-ups from task-creating Argus schedules.

#### Scenario: Promise to check back

- **WHEN** Solo promises a later follow-up
- **THEN** its orientation directs it to create a timer and keep open-loops memory consistent
