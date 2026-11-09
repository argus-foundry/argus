## 1. Implementation

- [x] 1.1 Add `agent.FreshSessionID`; use it in `CreateAndStart` step 4 and `restartSessionWithSeedPrompt`
- [x] 1.2 Ship `SessionID` on `RecycleReq` (client + sessioncore); bump `ProtocolVersion` and `SupervisorSpawnSurface`
- [x] 1.3 Tests: helper, BuildCmd `--session-id`, recycle persists a fresh ID, wire carries it
- [x] 1.4 Gotcha bullet in `daemon-rpc.md`
