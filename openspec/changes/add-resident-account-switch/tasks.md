## 1. DB and config

- [ ] 1.1 Tests then `db/resident_account.go`: override get/set/clear in resident meta; `SetResidentAccountMove` (account + session id + override in one tx, resident row only, refuses non-resident); `last_auto_switch_at` get/set.
- [ ] 1.2 `ResidentConfig.AutoSwitchAccount` (default true), db config key, config.toml override, Settings -> Resident toggle; extend config defaults test.

## 2. Move core

- [ ] 2.1 Tests then `agent.MoveResidentAccount` (allowResident mode in the shared core; generic entry points still refuse the resident; backend move still refuses it; no-transcript fresh case; busy refusal; persist-before-kick; restart-unknown error path).
- [ ] 2.2 Daemon RPC `Daemon.ResidentMoveAccount` + client method; serialization with the ensure loop.

## 3. Effective account and auto switch

- [ ] 3.1 Tests then effective-account helper used by `residentManager.start` (override wins; dropped on base change / invalid account; logs).
- [ ] 3.2 Tests then `maybeAutoSwitch` in `tick` (depleted+idle+2 ticks, mid-turn skip, needs-input does NOT block, in-flight skip, unknown/stale reading, depleted-target filter, none-eligible log/board dedup, persisted cooldown across a manager restart, setting off).
- [ ] 3.3 Notices: `[argus]` `ResidentSend` turn, board item (fyi / decision), uxlog + slog on every decision.

## 4. TUI

- [ ] 4.1 Tests then `>` on the resident row opens the picker (auto + Claude accounts only, no handoff rows) and calls the RPC; worker rows unchanged; SimulationScreen smoke test; keymap/help unchanged (`>` already bound) but confirm the resident tab help row.

## 5. Docs and gate

- [ ] 5.1 Gotchas in `context/knowledge/gotchas/resident.md` (override semantics, detection latency, move vs ensure loop, why no self-service) and the knowledge index row.
- [ ] 5.2 README Reference: `[resident] auto_switch_account`, `>` on Solo.
- [ ] 5.3 `GOFLAGS=-p=1 make pre-pr`; archive the change; open PR via iris.

## Non-goals (named follow-ups)

- `add-resident-self-switch`: self-only MCP tool with a deferred-intent flag (design D4).
- Screen-banner detection of a hit limit (design D3 limitation).
- Web and macOS surfaces: the resident has no web/macOS views yet.
- Remote TUI (`--remote`) has no resident tab.
- Automatic return to the preferred account after it recovers.
