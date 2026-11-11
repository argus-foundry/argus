# Tasks

## 1. Liveness predicate (tests first)
- [ ] 1.1 `agent.DisconnectedIDs`: table tests — fail-open on unhealthy list, 2-tick grace, scope (in_progress/in_review only), pendingRestart counted as running.
- [ ] 1.2 `daemon/client`: `SessionListHealthy()` set by `RunningAndIdle`; test RPC error → false.
- [ ] 1.3 Implement; wire in `App.onTick` next to `SetSessionRunning`; uxlog on set transitions.

## 2. Icons
- [ ] 2.1 `widget.RoleStatusIcon` `Disconnected` input + precedence; update `rolestatusicon_test.go`.
- [ ] 2.2 `RoleView.Disconnected` + `HeraPage.SetDisconnected` (changegate fingerprint includes it); plan-view node picks it up via the shared classifier.
- [ ] 2.3 `widget.TaskStatusIcon` `Disconnected` input; Tasks-tab list feed; jump switcher inherits via classifier.

## 3. Dead-pane marker
- [ ] 3.1 `TerminalPane` default dead-replay note + tests (precedence vs closed-out/launch/placeholder).

## 4. Live-agent correctness
- [ ] 4.1 Shared dead-handle re-resolve helper; Tasks-tab tick uses it.
- [ ] 4.2 Tasks-tab Enter: liveness check → reattach, no `startSession`, no Pending flip.
- [ ] 4.3 Stream backoff + uxlog; fake-dialer test.

## 5. Docs and wrap-up
- [ ] 5.1 Gotchas in `hera-view.md`, `tasklist-ui.md`, `pty-terminal.md`/`daemon-rpc.md`; README reference if icon legend exists.
- [ ] 5.2 `make pre-pr` once; archive change in the PR.
