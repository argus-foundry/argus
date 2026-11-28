# Keep exit notifications session scoped

## Why

The TUI can receive a daemon exit notification for a retired PTY after a replacement has started. A delayed notification can overwrite the replacement's state or clear its pane.

## What Changes

The daemon owns lifecycle persistence for daemon-backed sessions. The TUI applies a daemon exit notification to the session that produced it, preserving newer connections and their status. Standalone TUI sessions retain local exit handling.

The remote TUI carries its width decision to the daemon so owner-managed rerenders still honor committed history width and explicit retries. Web and macOS keep the same standard resize behavior.
