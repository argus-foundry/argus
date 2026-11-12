## ADDED Requirements

### Requirement: Supervisor-restart reconcile records orphans for recovery

When the supervisor-restart reconcile flips true orphans to in_review and bounce recovery is enabled, the daemon SHALL also record those orphans in the bounce recovery ledger (after the resume-time session ID refresh and before the recovery worker starts). It SHALL skip recording when the supervisor live-set query failed, since no orphan can be established.

#### Scenario: Orphans recorded

- **WHEN** the reconcile flips 9 true orphans
- **THEN** 9 ledger entries exist before the daemon accepts connections

#### Scenario: Live-set query failed

- **WHEN** the supervisor live-set query returns nil
- **THEN** no ledger entries are written
