## Why

The resident interrupts ordinary local work and connector reads with needless approvals. Aaron approved option B: keep confirmation for outward actions rather than bypass permissions.

## What changes

- Allow shipped built-ins, ordinary Argus tools and more connector reads.
- Preserve outward confirmation, Slack send denial, structural Hera restrictions and all launch isolation.
- Document unsupported global allow wildcards and the conservative unknown-tool fallback.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `resident-agent`: Widen shipped permission defaults without changing list overrides or launch enforcement.

## Impact

Config defaults and policy tests; all three frontends consume the same config without wire changes. Daemon and supervisor binaries must be updated, then the resident restarted.
