## Why

A deferred cleanup and a TUI confirmation both outlive the repository lookup that authorized them. A delayed PWA archive response can also outlive a later state change.

## What Changes

- Keep the captured repository fixed through resident cleanup and stack confirmation.
- Check current archival state before applying a delayed result to a newer detail visit.
