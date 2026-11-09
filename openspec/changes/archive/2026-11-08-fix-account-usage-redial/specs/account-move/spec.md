## ADDED Requirements

### Requirement: Account usage for moves survives a daemon bounce

The TUI SHALL read per-account usage for the `>` picker and the `auto` target over a fresh daemon connection when its long-lived client has failed, and SHALL treat usage as unknown only when the daemon is unreachable on that fresh connection too.

#### Scenario: Daemon bounced since the TUI started

- **WHEN** the TUI's original daemon connection is dead but the daemon is running
- **THEN** the picker and `auto` use the daemon's current per-account usage readings
