## 1. Client
- [x] 1.1 Add `ConnectLive`, `liveBootedAt`, `awaitSuccessor`; use in `autoStartFork`
- [x] 1.2 Tests with fake daemons (old owns socket → successor accepted)

## 2. TUI
- [x] 2.1 `reconnectLiveDaemon` before restart in the health check, with uxlog
- [x] 2.2 `gracefulDaemonShutdown` retries over a fresh connection
- [x] 2.3 Tests

## 3. Docs
- [x] 3.1 Gotcha bullet in `gotchas/daemon-rpc.md`
