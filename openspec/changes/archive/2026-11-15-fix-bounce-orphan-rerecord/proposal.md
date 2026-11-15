## Why

Daemon-only restarts repeatedly select already-dead Hera workers because durable role bindings are mistaken for evidence of newly lost sessions. Each pass overwrites the recovery ledger and sends another ARGUS_BOUNCED, indefinitely delaying expiry and discarding recovery progress.

## What Changes

- Require prior supervisor liveness or an unreconciled in_progress transition to identify a newly interrupted session; a live Hera binding alone is insufficient.
- Preserve existing ledger entries and suppress notifications for previously reconciled dead sessions, including dormant team members.
- Preserve repeat-reconcile safety and allow a genuinely resumed session lost in a later bounce to receive a fresh recovery entry.
- Log skips and document the evidence boundary, with regression coverage for repeated daemon starts.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bounce-recovery`: Select only newly interrupted sessions and retain previous recovery state across daemon-only restarts.

## Impact

Changes daemon startup reconciliation, agent candidate selection, and bounce ledger recording. No REST contract, keybinding, supervisor protocol, or frontend implementation changes; TUI, web, and macOS observe the same corrected daemon behavior. Binding-only recovery of stale-status tasks after a hard supervisor crash without liveness evidence is intentionally removed because it cannot distinguish pre-existing deaths.
