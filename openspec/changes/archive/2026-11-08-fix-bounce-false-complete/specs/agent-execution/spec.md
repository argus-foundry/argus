## ADDED Requirements

### Requirement: Finished-session check spares live coordinator and freelance sessions

The finished-session check SHALL NOT stop the session of a non-archived task that holds a live Hera coordinator or freelance binding, even when the task's status is complete, because those roles idle between messages by design and a complete label can be a false inference. Worker-bound tasks are unaffected. Archived tasks are still stopped. The first skip per task SHALL be logged naming the binding, role, kind and orchestrator, and a failure to look the binding up SHALL be logged and treated as keep.

#### Scenario: Complete coordinator is spared
- **WHEN** a task holding a live coordinator binding is complete and its session stays idle across consecutive checks
- **THEN** the session is not stopped and the skip is logged with the binding

#### Scenario: Freelance session is spared
- **WHEN** a task holding a live freelance binding is complete and idle across consecutive checks
- **THEN** the session is not stopped

#### Scenario: Ended binding is reaped as before
- **WHEN** the binding has ended and the session stays idle across two checks
- **THEN** the session is stopped

#### Scenario: Worker and archived tasks are still stopped
- **WHEN** a complete worker-bound task, or an archived coordinator task, stays idle across two checks
- **THEN** its session is stopped
