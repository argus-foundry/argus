## Why

Solo currently forces Manual/default permissions even though Aaron uses Claude Code auto mode manually. Launching in auto lets the safety classifier review ordinary actions while retaining the useful fixed allow/ask/deny policy.

## What changes

- Force resident auto mode with bypass disabled and backend permission flags stripped.
- Retain fixed rules, remove blanket Bash approval, and preserve Manual fallback when auto is unavailable.
- Log native fallback and detect classifier escalations and terminal missing-verdict stops.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `resident-agent`: launch permissions, fallback diagnostics and waiting detection.

## Impact

BuildCmd, inline Claude settings, resident defaults, PTY diagnostics and needs-input tests. No REST contract changes; TUI, web and macOS consume the same waiting state and terminal output. A supervisor restart and resident restart are needed. Direction approved by Aaron.
