## ADDED Requirements

### Requirement: Shared lifecycle service for bounce and forced recycle

Worker bounce and coordinator forced recycle SHALL be implemented once in a shared service used by both the TUI rail actions and the REST routes. The service SHALL re-validate role kind, live binding and live session before writing input or restarting, SHALL record a persistent receipt keyed by request id, SHALL advance a bounce to `restarted` when the role's own self-service recycle restarts it, and SHALL fail forced recycles left `restarting` across a daemon restart.

#### Scenario: Bounce completes through self-service recycle
- **WHEN** a bounced worker calls hera_status with request_recycle and the recycle watcher restarts it
- **THEN** the bounce operation becomes `restarted`

#### Scenario: Stale binding
- **WHEN** the role's live binding ended after the request was composed
- **THEN** the service rejects it without writing input
