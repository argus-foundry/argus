## 1. Setting (config-management)

- [ ] 1.1 Test first: `TestConfig_AutoRerenderOnResize_DefaultOff`, `_FileOverlayWins`, `_AbsentKeyKeepsDefault` in `internal/config`
- [ ] 1.2 Add `UIConfig.AutoRerenderOnResize` (`toml:"auto_rerender_on_resize"`, default false) and DB key `ui.auto_rerender_on_resize`
- [ ] 1.3 Test first: `TestSettingsBridge_AutoRerenderOnResize` (local toggle) and the apistore remote write path case in `internal/apistore`
- [ ] 1.4 Wire `internal/tui/settings_bridge.go` ("Auto re-render on resize") and `internal/apistore/store.go`
- [ ] 1.5 uxlog `[rerender] auto_rerender_on_resize=%t` at TUI start and on live change; test asserts the line

## 2. Gate the automatic kick (agent-view, hera-view)

- [ ] 2.1 Test first: `TestMaybeKickRerender_SettingOff_NoStop` and `TestMaybeKickRerender_SettingOn_IdenticalToday` (drift past margin, idle agent: no `StopScoped` when off, exactly one when on) in `internal/tui`
- [ ] 2.2 Add `App.autoRerenderOn()`; gate `maybeKickRerender` entry and the `kickWait` retry
- [ ] 2.3 Test first: `TestPanes_AutoRerenderOff_NoKickEvaluation`, `TestPanes_ZoomToggle_SettingOff_NoRearm`, `TestPanes_SettingOn_KickDebounceUnchanged` in `internal/tui/hera`
- [ ] 2.4 Add `HeraPage.SetAutoRerender(func() bool)` mirroring `SetKeepPlace`; gate `maybeKickPaneRerender`; keep the on-path byte-identical
- [ ] 2.5 Smoke test `TestSmoke_ResizeDoesNotRestart` (SimulationScreen: resize and Ctrl+Z twice, session PID unchanged, no kick log)

## 3. Owed notice on the pane frame (terminal-rendering)

- [ ] 3.1 Test first: `TestTerminalPane_RerenderOwedNotice_Precedence` (Re-rendering > Re-render pending > owed) and `_TruncatesBeforeTitle` in `internal/tui/terminal`
- [ ] 3.2 Add `TerminalPane.SetRerenderOwed(on, hint)`; extend the right-aligned border draw at `terminalpane.go:1996`; clear on `ResetVT`/rebind
- [ ] 3.3 Test first: `TestOwedPredicate_*` table (init anchor, committed anchor, margin boundary 14/15, setting on hides it) for the pure drift helper
- [ ] 3.4 Drive the notice from the classic view tick and from `HeraPage` Draw after `SetRect` (both panes independent); transition-only uxlog `[rerender] owed notice shown/cleared` with reason
- [ ] 3.5 Smoke tests `TestSmoke_OwedNoticeTopBorderRight_Tasks` and `_Hera` (SimulationScreen cell assertions: title left, notice right on the top border row, no content overwritten)

## 4. Hotkey (keybindings, command-palette)

- [ ] 4.1 Test first in `internal/tui/keymap`: `TestDefaults_AgentRerenderShiftHome`, `TestDefaults_NoCollision`, `TestAgentRerender_PlainKeyRejected`, and a regression `TestAgentCtx_ComposerChordsNotBound` (ctrl+a/e/u/w/k/o/b stay unbound in CtxAgent)
- [ ] 4.2 Add `ActAgentRerender` to `defaultSpecs`, `actionLabels`, `contextOrder` (CtxAgent)
- [ ] 4.3 Test first: `TestHelpModal_Draw` asserts the generated row; `TestPalette_AgentRerenderRow` for classic agent focus and Hera pane focus
- [ ] 4.4 Add the `keymap.Resolve` case in `handleAgentKey` and the Hera pane handler; implement `App.rerenderNow` (forced path: skips debounce/margin short-circuits, keeps idle/prompt/scroll/draft gates)
- [ ] 4.5 Tests `TestRerenderNow_Idle_Kicks`, `_PromptBlocked_Defers`, `_NoDrift_NoopNotice`, `_ScrolledUp_Defers`; uxlog `[rerender] hotkey ... outcome=` asserted
- [ ] 4.6 README Reference keybinding table row; `context/knowledge/gotchas/keybindings.md` note on the audit

## 5. Spawn size (agent-execution)

- [ ] 5.1 Test first in `internal/daemon`: `TestResolveSpawnSize_ReportBeatsPTY`, `_PTYWhenNoReport`, `_DefaultWhenNoSession`, with `t.Setenv("HOME", t.TempDir())`
- [ ] 5.2 Implement `resolveSpawnSize` and use it in `restartSessionWithSeedPrompt` (`recycle.go:106-129`) for the coordinator and plain-task runners; log `[spawn-size] recycle ... source=`
- [ ] 5.3 Audit `HeraReviveRunner.KickRerender` (`revive.go:76`) and the account-move kick for the same stale-size inheritance; add tests or record "already correct" in the gotchas
- [ ] 5.4 Verify `ProtocolVersion` is unchanged and `internal/daemon/recycle.go` is outside the supervisor spawn-surface digest set (re-record per `gotchas/ci-gates.md` only if it is)

## 6. Docs, parity, wrap-up

- [ ] 6.1 Gotchas: `gotchas/pty-terminal.md` + `gotchas/hera-view.md` (no-restart default, duplicated-rows limitation, drift predicate now drives a notice, notice shares the RerenderStatus slot), `gotchas/web-remote.md`/`macos-app.md` one-line pointer to the named follow-up `resize-without-restart-clients`
- [ ] 6.2 README Reference: setting row; no top-half edit
- [ ] 6.3 `make pre-pr` clean before pushing the implementation PR
- [ ] 6.4 Archive within the same PR: `openspec archive resize-without-restart` (or manual merge-and-move)
