## 1. Failing tests

- [ ] 1.1 backendtier: burst spreading, penalty expiry, unknown-tier LRU, pure ResolveChoice does not reserve
- [ ] 1.2 agent: implicit default persisted blank + routed; explicit default stays pinned; hera orchestrator account; fork account
- [ ] 1.3 mcp/api: blank/routed caller routes
- [ ] 1.4 TUI form `default` submits blank; Swift NewTaskAccount tests

## 2. Backend

- [ ] 2.1 Spawn ledger + `ResolveAndReserve` with injectable clock and `[routing]` logs
- [ ] 2.2 `create.go`/`routing.go`: reserve, routed flag, `[routing]` pin/auto log
- [ ] 2.2b `routing.go`: add step-3 best-overall fallback with `[routing]` step log + tests
- [ ] 2.3 `resolveOrchestratorAccount`, fork account helper (`app.go`, `handlers.go`)
- [ ] 2.4 MCP `taskCreateAccount` blank/routed callers route

## 3. Frontends

- [ ] 3.1 TUI newtaskform: `default` submits blank, label notes auto-routing
- [ ] 3.2 Web SPA `default` submits blank + bump `SW_VERSION`
- [ ] 3.3 macOS NewTaskAccount/NewTaskSheet `default` submits blank

## 4. Docs and wrap-up

- [ ] 4.1 gotchas/usage-budget-routing.md (+ Codex skew, pinned rows note), README Reference if tables change
- [ ] 4.2 `make pre-pr`, `make mac-test`, archive change, open PR
