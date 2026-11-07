## ADDED Requirements

### Requirement: A worker reporting working clears its close-out readiness

When a worker-kind role applies the status `working` through `hera_status` or `hera_send`, the daemon SHALL clear the `ready_to_close` mark on the role's bound task before committing the role status. It SHALL NOT change the task status. Coordinator and freelance roles SHALL NOT clear any task's mark. If clearing fails, the status SHALL NOT be applied and `hera_status` SHALL return an error; `hera_send` SHALL still deliver its message.

#### Scenario: Done then working clears readiness

- **WHEN** a worker reports `done` (task rolled to in_review, marked ready) and then reports `working`
- **THEN** the task's ready mark is cleared and its task status is unchanged

#### Scenario: Other roles are unaffected

- **WHEN** a coordinator, or a different worker, reports `working`
- **THEN** another worker's ready mark remains

#### Scenario: Clear failure

- **WHEN** clearing the mark fails
- **THEN** the role status is unchanged and `hera_status` returns an error

#### Scenario: Accepted worker replies before a daemon bounce

- **WHEN** a worker reports done, is accepted, replies with status working and remains alive across a daemon bounce
- **THEN** automatic reattach leaves its task complete; reopening requires explicit revive
