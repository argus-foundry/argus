## Why

A zero expected timestamp becomes the legacy empty revision sentinel and bypasses strict dismissal checks. Decoding only the first JSON value also allows trailing invalid data and oversized trailing whitespace to mutate the board.

## What Changes

Reject zero expected timestamps in the API and both strict store entry points. Require EOF through the bounded request reader before either board mutation. Cover unchanged state, no notices/events, valid bounded whitespace, and preserved legacy resolution.

## Non-Goals

No legacy TUI/MCP signature changes, macOS changes or ENG-1125 event relay implementation.
