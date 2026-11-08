## 1. Probe root cause

- [x] 1.1 Reproduce detach-then-probe against a real supervisor
- [x] 1.2 Ask the supervisor before reporting a cached handle dead; error when unreachable
- [x] 1.3 Genuine-exit direction still reports not alive

## 2. Accept ends the coordinator binding

- [x] 2.1 db: end and restore accepted coordinator bindings
- [x] 2.2 AcceptRole ends it; revive and every resume path restores it
- [x] 2.3 Tests for accept, revive, dead auto-revive, TUI, REST, failure reporting

## 3. Docs

- [x] 3.1 Gotcha and index; re-record stream digest (no surface bump)
