## Decisions

- **Sidecar, not in-band.** Offsets are log-file bytes (counter bumped after each log write), the one coordinate every replay reader shares. Append-only lines make a crash harmless: a torn final line is dropped, backwards offsets are skipped.
- **Writer.** `Session.Resize` in the PTY-owning process (the supervisor). Deduped on size change so a drag adds a line only when the size actually changes. An old supervisor writes no index; replay falls back.
- **Skew.** Output the kernel already buffered before a resize but not yet read lands after its mark; bounded by the PTY buffer, shared by any mechanism.
- **Replay.** `feedReplayChunksSized` cuts chunks at marks (the parser is stateful, so a mid-sequence cut is safe), resizes at each, and resizes back to the build size at the end. x/vt Resize crops visible rows when narrowing and keeps scrollback lines at full width (spiked), matching what the live emulator did.
- **Ring-only fallback** (no log) never uses the index: its origin is meaningless.
