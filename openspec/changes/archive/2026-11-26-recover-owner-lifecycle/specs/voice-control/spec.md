## ADDED Requirements

### Requirement: Reclaim recovers the retiring process

After daemon restart or lost delivery, the daemon SHALL recover a retained confirmed exit for an armed reclaim PID. It SHALL preserve the worktree while the process or its exit remains unverified. Reclaim SHALL verify that no live Hera binding remains before acting. PID-scoped stops SHALL signal only the approved process tree.

#### Scenario: The daemon misses a confirmed reclaim exit

- **WHEN** a daemon restarts after the supervisor observes the armed process exit
- **THEN** reconciliation finalizes that exact exit and reclaims the eligible task

### Requirement: Owner-managed restart remains observable

An owner that cannot report the managed successor PID SHALL refuse newly requested managed restarts. A confirmed restart that is still in owner prelaunch SHALL remain tracked independently of retries for a dropped transport.

#### Scenario: A managed restart spends a long time in prelaunch

- **WHEN** the reported pending restart outlasts the reconnect budget
- **THEN** the client still receives the owner's final result

### Requirement: Terminal cursors stay with their process

A live terminal snapshot SHALL identify its process alongside the cursor. Clients SHALL pass that identity when attaching the live stream and refresh their snapshot if the process changed.

#### Scenario: A session changes between snapshot and attachment

- **WHEN** the current process differs from the one associated with the requested output cursor
- **THEN** the client opens a fresh snapshot without reusing the outgoing cursor
