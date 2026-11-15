## MODIFIED Requirements

### Requirement: Timer failure isolation and retention

A timer whose next recurrence cannot be calculated SHALL be deactivated and logged without blocking healthy timers or delivery retries. The daemon SHALL process pending deliveries and cancellations even if firing reports an error. Terminal personal timer rows older than 30 days SHALL be pruned once their delivery receipts are settled; undelivered events MUST survive extended downtime. Config-owned built-in identities MUST be excluded from pruning, including when disabled by invalid-recurrence recovery. Timer state-read errors SHALL be logged.

#### Scenario: Broken recurrence alongside healthy work

- **WHEN** a due recurring timer has an invalid zone or no next occurrence
- **THEN** it is deactivated and other timers, pending deliveries and cancellations continue

#### Scenario: Old terminal history

- **WHEN** a completed or cancelled personal timer has been terminal for more than 30 days and has no unsettled receipt
- **THEN** its timer row is deleted while active, unsettled and config-owned built-in timers are preserved
