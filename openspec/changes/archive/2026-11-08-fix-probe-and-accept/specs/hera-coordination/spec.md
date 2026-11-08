## ADDED Requirements

### Requirement: Accepting a sub-coordinator ends its coordinator binding; revive restores it

When hera_accept (or the plan-DAG gater's auto-accept) marks a role's task complete, it SHALL also end every live coordinator-kind binding on that task, stamped with end reason accepted, so the finished-session check can stop the idle session. Worker and freelance bindings SHALL be left live so the role stays messageable and revivable. A failure to end the binding SHALL be reported with the accept (the task still completes and the check-in is still sent). Reviving or resuming the task (hera_revive, hera_send's dead auto-revive, TUI resume including the third-Enter revive, REST resume and restart) SHALL re-open the most recent accepted-ended coordinator binding, keeping the same binding, provided its role and orchestrator are not archived or nuked and the task holds no live binding in that orchestrator; a restore failure SHALL be reported to the caller, not swallowed. A daemon-bounce reattach SHALL NOT restore it.

#### Scenario: Accepted sub-coordinator's coordinator hat ends
- **WHEN** a coordinator accepts a sub-coordinator's worker role
- **THEN** the task becomes complete, its coordinator binding ends with reason accepted, and its worker binding stays live

#### Scenario: Revive restores the coordinator hat
- **WHEN** an accepted sub-coordinator's task is revived or resumed
- **THEN** its coordinator binding is live again and the revived session is a working coordinator

#### Scenario: Restore failure is reported
- **WHEN** the session was restarted but the coordinator binding could not be restored
- **THEN** the revive reports that failure instead of succeeding silently

#### Scenario: Plain workers are unaffected
- **WHEN** a worker with no coordinator binding is accepted
- **THEN** acceptance behaves exactly as before
