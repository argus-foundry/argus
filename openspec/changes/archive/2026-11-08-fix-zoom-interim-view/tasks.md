## 1. Tests

- [x] 1.1 Real-log fixture: replay of a shrunk live session has no fragment rows; control proves the detector discriminates
- [x] 1.2 replayEmuDims unit test; zoom dwell and backoff-reset test; backoff cap test

## 2. Implementation

- [x] 2.1 replayEmuDims takes the initial PTY width for live sessions
- [x] 2.2 ZoomKickDebounce, retryDelay reset on zoom, kickRetryMax 15s, uxlog

## 3. Docs

- [x] 3.1 Gotchas (pty-terminal, hera-view) and knowledge index
- [x] 3.2 Archive the change into the base specs
