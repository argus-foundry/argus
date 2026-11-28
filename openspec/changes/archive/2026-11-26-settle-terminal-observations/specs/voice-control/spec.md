## ADDED Requirements

### Requirement: Terminal observation and reclaim completion

Owner observations SHALL pair the process slot with its last confirmed exit. Recovery SHALL preserve owner identity through delayed exit delivery. Hera SHALL retain reclaim ownership until terminal retirement is settled.

#### Scenario: An ordinary reserved process retires before attachment
- **WHEN** a consumer observes a launch reservation and the process exits before attachment
- **THEN** the consumer waits for the process's retained exact exit before settling it.

#### Scenario: PWA sees stale snapshots during replacement
- **WHEN** a snapshot remains stale while a replacement has yet to settle
- **THEN** the terminal stays in a single bounded recovery loop.

#### Scenario: Hera's receipt payload expires
- **WHEN** no live binding exists and the owner confirms both retirement and the exact armed PID as its most recent exit
- **THEN** reclaim clears its intent without fabricating a terminal event or exit code.

#### Scenario: Nuke arrives during managed prelaunch
- **WHEN** the archive and role transition commit while an active launch temporarily prevents a scoped stop
- **THEN** the daemon retries the stop after the owner settles, and generic relaunch and pruning wait for reclaim.
