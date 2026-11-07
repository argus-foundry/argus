# Fix workspace attachment submission

## Why
Claude Code keeps a mixed text and image path paste literal. Waiting for conversion prevents Enter and leaves stale browser drafts.

## What Changes
Accept literal paths after bounded paste settling, send one standalone CR, and clear after daemon input acceptance. Remove speculative Enter retries and rendering-based acknowledgment.

## Impact
Optional workspace frontend and tests only. Existing TUI, original web UI and macOS REST contracts remain unchanged.
