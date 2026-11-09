## 1. Failing tests

- [x] 1.1 backendtier: burst spreading, penalty expiry, unknown-tier LRU, pure ResolveChoice does not reserve
- [x] 1.2 agent: implicit default persisted blank + routed; explicit default stays pinned; hera orchestrator account; fork account
- [x] 1.3 mcp/api: blank/routed caller routes
- [x] 1.4 TUI form `default` submits blank; Swift NewTaskAccount tests

## 2. Backend

- [x] 2.1 Spawn ledger + `ResolveAndReserve` with injectable clock and `[routing]` logs
- [x] 2.2 `create.go`/`routing.go`: reserve, routed flag, `[routing]` pin/auto log
- [x] 2.2b `routing.go`: add step-3 best-overall fallback with `[routing]` step log + tests
- [x] 2.3 `resolveOrchestratorAccount`, fork account helper (`app.go`, `handlers.go`)
- [x] 2.4 MCP `taskCreateAccount` blank/routed callers route

## 3. Frontends

- [x] 3.1 TUI newtaskform: `default` submits blank, label notes auto-routing
- [x] 3.2 Web SPA `default` submits blank + bump `SW_VERSION`
- [x] 3.3 macOS NewTaskAccount/NewTaskSheet `default` submits blank

## 4. Docs and wrap-up

- [x] 4.1 gotchas/usage-budget-routing.md (+ Codex skew, pinned rows note), README Reference if tables change
- [x] 4.2 `make pre-pr`, `make mac-test`, archive change, open PR
