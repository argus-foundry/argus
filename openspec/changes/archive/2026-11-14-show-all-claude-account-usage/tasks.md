## 1. Status bar

- [x] 1.1 Add per-account Claude entries to `StatusBar` (`SetClaudeAccountUsage`) and render them between `cla` and `cdx`
- [x] 1.2 Widget tests: known/unknown entries, suffixes, no-accounts segment unchanged

## 2. Polling

- [x] 2.1 `reevaluateUsage` reads per-account usage for configured non-default Claude accounts and feeds the status bar
- [x] 2.2 TUI test: a named account's reading lands on the status bar; a missing one renders `—`

## 3. Close out

- [x] 3.1 `make pre-pr`
- [x] 3.2 Archive the change into `openspec/specs/tui-shell/spec.md`
