## Why

Solo needs deliberate, quiet maintenance of its own memory now that durable resident timers can wake its session. The shared KB dreaming pass leaves this folder to Solo.

## What changes

- Ensure one daemon-owned nightly memory tidy timer, configured by `resident.memory_tidy` (default `02:30` local; `off` disables).
- Deliver a complete curation procedure with each fire; protect explicit instructions and preferences.
- Distinguish the built-in timer from Solo's timers and refuse agent cancellation.

## Capabilities

### New capabilities

- `resident-memory-tidy`: Configured, durable, quiet self-curation of resident memory.

### Modified capabilities

- `resident-timers`: Distinguish config-owned built-ins and apply the active cap to personal timers.

## Impact

Resident config, timer persistence, daemon delivery, orientation, tests and documentation. Existing resident timer tools expose ownership; no new endpoints or controls.
