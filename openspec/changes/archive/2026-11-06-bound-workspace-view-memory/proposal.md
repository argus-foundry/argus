# Bound workspace view memory

## Why
Visited terminals and old diff bodies accumulate across task navigation.

## What Changes
Keep at most eight terminal controllers in least recently used order. Protect the selected and split-visible task. Destroy evicted controllers and restore revisits through a fresh bounded snapshot. Keep review selection metadata without inactive diff bodies. Preserve drafts and input cancellation.

## Impact
Optional web workspace only. No REST, Go, TUI or native macOS changes. Existing native macOS cache behavior is outside this optional client fix.
