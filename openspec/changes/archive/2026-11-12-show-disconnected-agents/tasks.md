# Tasks

## 1. Liveness predicate (tests first)
- [x] 1.1 `agent.DisconnectedIDs`: table tests — fail-open on unhealthy list, 2-tick grace, scope (in_progress/in_review only), pendingRestart counted as running.
- [x] 1.2 `daemon/client`: `SessionListHealthy()` set by `RunningAndIdle`; test RPC error → false.
- [x] 1.3 Implement; wire in `App.onTick` next to `SetSessionRunning`; uxlog on set transitions.

## 2. Icons
- [x] 2.1 `widget.RoleStatusIcon` `Disconnected` input + precedence; update `rolestatusicon_test.go`.
- [x] 2.2 `RoleView.Disconnected` + `HeraPage.SetDisconnected` (changegate fingerprint includes it); plan-view node picks it up via the shared classifier.
- [x] 2.3 `widget.TaskStatusIcon` `Disconnected` input; Tasks-tab list feed; jump switcher inherits via classifier.

## 3. Dead-pane marker
- [x] 3.1 `TerminalPane` default dead-replay note + tests (precedence vs closed-out/launch/placeholder).

## 4. Live-agent correctness
- [x] 4.1 Shared dead-handle re-resolve helper; Tasks-tab tick uses it.
- [x] 4.2 Tasks-tab Enter: liveness check → reattach, no `startSession`, no Pending flip.
- [x] 4.3 Stream backoff + uxlog; fake-dialer test.

## 5. Docs and wrap-up
- [x] 5.1 Gotchas in `hera-view.md`, `tasklist-ui.md`, `pty-terminal.md`/`daemon-rpc.md`; README reference if icon legend exists.
- [x] 5.2 `make pre-pr` once; archive change in the PR.
