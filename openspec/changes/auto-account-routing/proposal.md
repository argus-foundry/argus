## Why

Argus does not spread Claude usage across Aaron's accounts. Account-aware tier routing exists and the non-default account is probed, but it almost never gets a chance to act: the new-task form (TUI/web/macOS) preselects `default` and submits it as an explicit account, which pins every Claude tier to `default`; hera workers inherit that pin; MCP `task_create` pins children of an implicit-default caller to `default`; and headroom routing is winner-take-all on a ~30-minute usage snapshot, so a burst of spawns all lands on one account. Observed 2026-11-07: a coordinator stored `default`, all 7 workers went to `default` at 70% while `personal` sat at 7%.

## What Changes

- Add an **Auto** account choice to the new-task form in all three frontends (TUI, web, macOS). It is the default selection and submits a blank account (unpinned → routed by headroom). Choosing a named account or `default` still pins.
- Persist an unpinned task as blank account + `account_routed=true` (even when routing lands on `default`), so children route too.
- MCP `task_create` inheritance, hera `resolveOrchestratorAccount`, and forks treat a blank/routed parent as routable; only a deliberately pinned (named or explicit `default`) parent account is inherited.
- `task_create` and `POST /api/tasks` accept `account: "auto"` as an alias for blank.
- Burst spreading: `ResolveChoice` adds an in-process recent-spawn penalty (5 usage points per spawn on that backend+account within 30 minutes), recorded atomically with the resolution at task creation, so N rapid spawns spread. Applies to known readings; among unknown readings the least-recently-used identity wins.
- Codex-vs-Claude threshold skew (95 vs 80): **out of scope** — `ThresholdPct` is a per-tier user setting and headroom is intentionally threshold-relative; documented in gotchas.
- Existing pinned rows: **no migration code.** Pinned rows stay pinned (we cannot tell an accidental `default` pin from a deliberate one); documented as a one-off `account-move auto` option.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `backend-tier-routing`: unpinned-account semantics, implicit-default persistence and inheritance, burst spreading.
- `forms-and-modals`: Account selector gains a default-selected Auto entry.
- `mcp-server`: `task_create` account inheritance treats blank/routed callers as routable and accepts `auto`.

## Impact

- Code: `internal/backendtier/choice.go` (+ new spawn ledger), `internal/agent/{create,routing,hera_spawn}.go`, `internal/mcp/server.go`, `internal/api/handlers.go`, `internal/tui/{newtaskform,app}.go`, `internal/api/static/{index.html,sw.js}`, `macos/Sources/{Argus,ArgusKit}`.
- Wire: REST `account` already optional; `"auto"` is a new accepted alias. No schema change.
- Frontend parity: all three clients updated in this PR.
