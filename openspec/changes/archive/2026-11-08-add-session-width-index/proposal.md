## Why

A session log is one process lifetime, but the PTY can be resized many times inside it (drag, Ctrl+Z). The replay emulates the whole log at one width (since fix-zoom-interim-view, the widest). That is wrong for segments authored at a narrower width: an agent's in-place redraw uses relative cursor moves sized by its own wrap count, so in a wider emulator the same bytes walk onto the wrong rows and erase or duplicate content (the overlap family of Hera garbling).

## What Changes

- The session writer records every PTY size change in an append-only sidecar `~/.argus/sessions/<task>.sizes` (`<logOffset> <cols> <rows>`), in the log file's own byte space, truncated with the log at session start. The log stream, ring buffer and daemon wire are unchanged: no ProtocolVersion or surface-digest bump.
- The TUI replay feeds the log in segments, resizing the emulator at each recorded offset, then returns it to the build width so paint and anchor math are unchanged. A window starting mid-log begins at the width in force at its start.
- Missing, torn or stale-shaped indexes fall back to the single-width replay.

## Non-Goals

- Live-follow emulator path (it already resizes in place as the PTY does).
- The dead-session preview (PreviewVT) and the remote/REST TUI, web and macOS clients: no REST-visible change; they keep the single-width behavior. A REST endpoint for the index is a named follow-up if remote replay needs it.
- An in-band log marker (rejected: it would put bytes in the log that are not in the ring, skewing the log/ring offset arithmetic).
