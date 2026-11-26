# Fence committed reclaim and terminal exits

## Why

Reclaim must identify the binding that authorised cleanup. Browser detail actions may outlast logout or a resumed task's view.

## What Changes

- Record whether each nuke actually acquired the task and verify deferred TUI ownership.
- Keep failed archive-role actions visible and reject missing task pins before lock allocation.
- Cancel terminal work on logout and bind resume completion to its initiating view.
