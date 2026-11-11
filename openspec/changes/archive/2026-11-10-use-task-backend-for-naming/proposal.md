# Use the task's backend for auto-naming

## Why
Auto-naming currently invokes Claude/Haiku for every task. Tasks running another backend therefore still require Claude and consume its quota.

## What changes
- Select the naming provider from the task's final resolved backend, after default/project/usage routing, rather than hardcoding Claude.
- Declare backend-specific non-interactive naming support in the backend family registry; use isolated, bounded utility invocations rather than launching an interactive task session.
- Preserve the task's resolved account/authentication context where applicable.
- Keep existing validation, timeout, retry, compare-and-swap rename, and fail-open behavior. Unsupported or unavailable providers keep the original slug; never silently fall back to Claude.
- Keep Claude's existing lightweight Haiku invocation and budget cap for Claude tasks; use backend-appropriate lightweight naming invocations for other supported families.

## Frontend parity
Naming runs in the shared creation path, so TUI, web, and macOS task creation receive the same behavior without client or REST schema changes.

## Non-goals
- Changing task backend routing or model selection for the task session.
- Adding interactive naming controls or a separate naming-provider preference.
- Requiring unsupported/custom CLI families to implement naming; those retain their fallback slug. Named follow-up: verify isolated low-cost naming invocations for Pi, OpenCode, Antigravity, Cursor, and OMP; this change supports Claude and Codex only.

## Approval
Approved by the user. Native macOS naming was evaluated locally and rejected as the default because of inconsistent specificity; no native naming fallback is included.
