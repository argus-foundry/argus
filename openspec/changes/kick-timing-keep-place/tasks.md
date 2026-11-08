## 1. Decision predicate

- [ ] 1.1 Add `scrolledUp`/`hasDraft` inputs and `RerenderDeferScrolled`/`RerenderDeferDraft` to `agent.ShouldKickRerender`; table tests
- [ ] 1.2 Mirror the draft gate in `Server.maybeKickRerender` (API) with tests

## 2. Draft signals

- [ ] 2.1 Pure typed-since-submit helper in `agentview` + tests (CSI ignored, paste, CR/Ctrl+C/Ctrl+U)
- [ ] 2.2 `RemoteSession.ComposerDirty()` via optional interface + tests
- [ ] 2.3 Bridge hub draft storage, `draft` event, `has_draft`, `bridge/draft` pull (one-shot, boot/age rules) + tests

## 3. TUI gates

- [ ] 3.1 `maybeKickRerenderAtWidth` scroll + draft gates, uxlog, anchor preservation
- [ ] 3.2 Tasks-view `kickWait` retry in `onTick`
- [ ] 3.3 Hera `maybeKickPaneRerender` scroll hold + dwell restart; zoom re-arm test
- [ ] 3.4 Smoke test through the real event loop

## 4. Mod

- [ ] 4.1 `prompt.change` hook with debounce, immediate empty flush
- [ ] 4.2 Restore pull on `session.start`, read-then-fill-if-empty, outcome report
- [ ] 4.3 Mod tests; re-record the supervisor spawn-surface digest

## 5. Setting and docs

- [ ] 5.1 `BridgeConfig.KeepPlace`, DB key, Settings row, REST/apistore wire, tests
- [ ] 5.2 Gotchas (`hera-view.md`, `pty-terminal.md`, `bridge.md`, `ci-gates.md` if needed), knowledge index, README Reference
- [ ] 5.3 `make pre-pr`, `/hera-review`, `/hera-review-test-adversary`, archive, PR
