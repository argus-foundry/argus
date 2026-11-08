# Fix workspace accessibility

## Why
The workspace has silent load failures, lost focus during layout and Settings changes, and tooltips that cannot be hovered.

## What Changes
- Announce asynchronous load failures without moving focus.
- Preserve equivalent layout toggle focus and Settings navigation focus.
- Retain informative tooltips while their owner or bubble is hovered or the owner is focused, with a short crossing grace and Escape dismissal.

## Impact
Optional web workspace only. No REST, Go, TUI, original web or macOS changes.
