## Context

Resident and Projects reuse one HeraPage. The shell must intercept boundary arrows before pane input reaches the PTY.

## Goals / non-goals

- Add resident to the enabled local TUI chain using the existing focus machine.
- Preserve toggle, numeric shortcuts, last-tab persistence and remote behavior.
- No REST, web or macOS changes: this is local TUI navigation only.

## Decisions

Handle resident boundaries in handleCrossTabArrow and delegate internal steps to HeraPage. Use the visible resident tab label to gate entry, matching switchTab. Switch first, then select the rightmost pane because resident entry rebuilds its layout.

## Risks / trade-offs

- Hidden panes can change the ladder: use ToRightmost and AtRightmost rather than hard-coded pane assumptions.
- PTY input can swallow arrows: verify with a live fake adapter through the event loop.
