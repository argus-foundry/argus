## 1. Daemon

- [x] 1.1 Tests then handler: `resizeReq` fields, `deferred` response, gate order, flag cap clock, screen read on evaluation path only, setting off
- [x] 1.2 uxlog for every deferral and cap release

## 2. Web SPA

- [x] 2.1 Pure `composerTyped` fold, scrolled/dirty fields on resize, deferral retry with stop conditions
- [x] 2.2 Bump `SW_VERSION`

## 3. macOS

- [x] 3.1 ArgusKit `ComposerTyped` + `resize(... scrolledUp:composerDirty:)` + `ResizeResult.deferred`, tests via `make mac-test`
- [x] 3.2 TerminalController wiring: scroll signal, send-path fold, retry/stop

## 4. Docs and ship

- [x] 4.1 README REST row, gotchas, knowledge index
- [x] 4.2 Targeted tests at -p=2, `/hera-review` + `/hera-review-test-adversary`, archive in the PR, push via iris, open PR
