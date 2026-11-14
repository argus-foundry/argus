## Why

Cross-project Hera plan nodes inherit branches from another repository and repeatedly fail worktree creation. They must use the target project's configured default when a derived base is unsuitable.

## What Changes

- Validate gater-derived bases against the target repository after best-effort fetch and reject source tasks from other projects.
- Apply the fallback before routing worker and sub-coordinator nodes.
- Preserve same-project stacking and explicit spawn-worker branch failures.
- Correct MCP descriptions and document the branch-selection invariant.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hera-coordination`: Constrain automatic plan stacking to valid target-project branches.

## Impact

Shared daemon gater, agent git helper, MCP descriptions, regression tests. TUI, web, and macOS all consume the same resulting tasks; no client wire changes or parity gaps.
