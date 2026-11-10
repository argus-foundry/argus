## MODIFIED Requirements

### Requirement: A task can be moved to another account

A failed kick SHALL retain the persisted target account and routed flag because the replacement may still start. It SHALL return an unknown restart error without claiming rollback or falling back to a second start based on a bare liveness read.

#### Scenario: Kick response lost
- **WHEN** a kick returns an error, whether or not the source handle looks alive
- **THEN** the target account remains persisted and no second launch is attempted
