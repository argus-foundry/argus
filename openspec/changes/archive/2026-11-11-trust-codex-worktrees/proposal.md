## Why

Codex 0.167.0 still asks for folder trust with approval/sandbox bypass enabled. Real CLI probes found a process-scoped inline projects-table override suppresses the dialog without changing the shared account config file.

## What changes

- Add Codex worktree trust at launch through its family registry, for fresh and resumed sessions.
- Deliver a single inline projects table instead of a quoted dotted CLI key, which misparses path dots in this installed version.
- Move the shared default-on switch from `[claude] trust_worktrees` to `[agents] trust_worktrees` in this unmerged feature.
- Preserve account config files and the CODEX_HOME overlay; fail open if canonicalizing the task path fails.

## Capabilities

### New capabilities

- `codex-worktree-trust`: Process-scoped trust for the current worktree on every Codex launch.

### Modified capabilities

- `claude-worktree-trust`: Use the backend-neutral off switch.

## Impact

Backend registry, BuildCmd, config defaults and reference docs. All daemon clients inherit the same behavior; no frontend UI or REST contract change. Codex launch changes require supervisor spawn surface 47; restart an older supervisor to receive the override. The previous Codex trust provisioning follow-up is resolved by this inline-table approach.
