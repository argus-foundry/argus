## 1. DB and config

- [x] 1.1 `db/resident_account.go`: marker get/set/clear, `SetResidentAccountMove` (account + session id + marker in one tx, resident row only), `last_auto_switch_at`; tests.
- [x] 1.2 `ResidentConfig.Accounts` (replaces `Account`), `auto_switch_account`, DB keys (`resident.accounts` JSON, legacy `resident.account` read as one element), config.toml overlay + `ResidentAccountsDefined`; tests.

## 2. Move core and plan

- [x] 2.1 `agent.MoveResidentAccount` (allowResident mode of the shared core; generic entry points still refuse; fresh/no-transcript case; busy refusal; persist-before-kick); tests.
- [x] 2.2 `agent.ResidentAccountEntries` / `ResidentStartAccount` / `DecideResidentAccount` (list order, `auto`, depleted incl. weekly, move-back-up, manual hold); table tests.
- [x] 2.3 Daemon RPC `Daemon.ResidentMoveAccount` + client method.

## 3. Daemon

- [x] 3.1 `residentManager.start` uses the persisted-account-plus-list rule.
- [x] 3.2 `maybeAutoSwitch`: parked check (raw or content-idle), needs-input never blocks, 2 ticks, persisted 10-min cooldown, forced move after the check, none-usable board item once per episode; tests incl. the weekly-limit wait-footer fixture.
- [x] 3.3 Notices: `[argus]` continue turn, `fyi`/`decision` board items, uxlog + slog.

## 4. TUI

- [x] 4.1 `>` on the resident row (own-row-only keymap policy), Claude-only picker, RPC route; smoke tests.
- [x] 4.2 Settings -> Resident -> Accounts row + ordered multi-select modal, config.toml read-only, `auto_switch_account` toggle; tests.

## 5. Docs and gate

- [x] 5.1 Gotchas in `resident.md` + index row; README Reference.
- [ ] 5.2 `GOFLAGS=-p=1 make pre-pr` (vuln stdlib advisory is CI-advisory); archive; PR via iris.

## Non-goals (named follow-ups)

- `add-resident-self-switch`: self-only MCP tool with a deferred-intent flag (design D4).
- Screen-banner detection of a hit limit (design D3 limitation).
- Web and macOS surfaces: the resident has no web/macOS views yet.
- Remote TUI (`--remote`) has no resident tab; the accounts editor is local-only.
