## Why

Agent browser work currently opens visible Google Chrome windows on Aaron's Mac, and live agents can leave those windows open indefinitely while waiting for review. The existing orphan reaper works, but cannot distinguish a finished browser job from a still-running agent.

## What changes

- Default sandboxed Claude and Codex browser automation to a pinned Playwright Chromium headless shell: no automation window or Dock icon during ordinary browser jobs, and no fallback to installed Google Chrome.
- Keep visible bundled Chromium available only through an explicit headed request. Close owned contexts when browser work finishes or is cancelled; use a configurable inactivity timeout as a fallback, including while a task is in review.
- Protect in-flight browser operations, resumed generations, personal Chrome, Claude-in-Chrome, and other attached browsers. Task status alone is never evidence that a browser is idle.
- Add a shared managed launcher and task-scoped MCP delivery, resolving known user/project/plugin browser-server conflicts without changing their source files or unrelated tools.
- Harden process scanning so one vanished or unreadable candidate cannot block another eligible orphan. Recognize the selected bundle's real executable and helper names.
- Record task, launch generation, browser engine/build, process identity, mode, and cleanup reason without browser content, credentials, raw arguments, or URLs.
- Verify both daemon and supervisor runtime builds during deployment; a replaced binary on disk is not sufficient evidence.

## Capabilities

### New capabilities

- `managed-agent-browser`: Shared pinned launch policy, browser-job lifecycle, inactivity protection, explicit headed use, direct automation helper, and redacted telemetry.

### Modified capabilities

- `mcp-injection`: Task-scoped managed browser delivery and precedence for sandboxed Claude/Codex launches, preserving unrelated servers and attached-browser integrations.
- `task-browser-cleanup`: Candidate-local scan failures, exact bundle recognition, natural-exit cleanup, and generation-safe integration with managed ownership.

## Impact

- Go: backend-family launch hooks, agent session lifecycle, config, browser process scanner, daemon/supervisor handoff, doctor, CLI entry point, and routing guidance.
- Managed runtime: a small Node/TypeScript Playwright adapter with exact dependency lock and matching browser revision; shipped/materialized with Argus, with an explicit installation/check path rather than runtime `@latest` resolution.
- Shared task controls in TUI, web, and macOS retain their existing REST contract. Browser timeout is a validated Argus config setting; dedicated browser settings UI across all three clients is a named follow-up, `browser-policy-settings-parity`.
- Out of scope: editing ai-ron global settings, Sketch, MerchantUI, sherlock, or the official Playwright plugin. The PR will list the exact alignment each needs for sessions outside Argus. Existing unmanaged live browsers are not retroactively idle-reaped without operation evidence.
- Approval gate: this change is a proposal only. No implementation or live browser launch/cleanup experiments until Aaron explicitly approves it.
