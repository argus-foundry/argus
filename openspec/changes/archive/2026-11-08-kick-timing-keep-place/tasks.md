## 1. Decision predicate

- [x] 1.1 `ShouldKickRerenderKeepPlace` wrapper with `RerenderDeferScrolled`/`RerenderDeferDraft`; table tests
- [x] 1.2 Mirror the draft gate in `Server.maybeKickRerender` (API) with tests

## 2. Draft signals

- [x] 2.1 Pure typed-since-submit helper in `agentview` + tests (CSI ignored, paste, CR/Ctrl+C/Ctrl+U)
- [x] 2.2 `RemoteSession.ComposerDirty()` via optional interface + tests
- [x] 2.3 Bridge hub draft storage, `draft` event, `has_draft`, `bridge/draft` pull (one-shot, boot/age rules) + tests

## 3. TUI gates

- [x] 3.1 `maybeKickRerenderAtWidth` scroll + draft gates, uxlog, anchor preservation
- [x] 3.2 Tasks-view `kickWait` retry in `onTick`
- [x] 3.3 Hera `maybeKickPaneRerender` scroll hold + dwell restart; zoom re-arm test
- [x] 3.4 Smoke test through the real event loop

## 4. Mod

- [x] 4.1 `prompt.edit` + `prompt.change` hooks with debounce, immediate empty flush; `draft_aware` on session.start
- [x] 4.2 Restore pull on `session.start`, read-then-fill-if-empty, outcome report
- [x] 4.3 Mod tests; add `draft.ts` to `SupervisorSpawnPaths`, bump spawn surface (38 after merging master), re-record the digest

## 5. Setting and docs

- [x] 5.1 `BridgeConfig.KeepPlace`, DB key, Settings row, REST/apistore wire, tests
- [x] 5.2 Gotchas (`hera-view.md`, `pty-terminal.md`, `bridge.md`, `ci-gates.md` if needed), knowledge index, README Reference
- [x] 5.3 `make pre-pr`, `/hera-review`, `/hera-review-test-adversary`, archive, PR
