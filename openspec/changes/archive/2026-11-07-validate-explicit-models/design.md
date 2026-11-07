## Decisions

- **Validate at CreateAndStart, not inside ResolveModel.** `ResolveModel` has no error return and runs on restart/kick/recycle for persisted tasks, where a hard error would strand a task after a backend config edit. Creation is the single point every client shares (REST JSON, multipart, headless/MCP/scheduler, TUI), so rejecting there stops silent fall-through at the source. Deviation from the ticket's "strict in ResolveModel" wording.
- **Resolved backend**: validation runs after backend-tier routing resolves the backend, using the merged config (`db.Config()`), before account resolution and any side effect.
- **Policy source**: `agent.ModelPolicy` reads `Backend.Models` then the family registry's `KnownModels`; no backend-identity branching outside `backends.go`.
- **`/api/backends` overlay**: the DB roster has no `models` column; `models` come from `config.toml`. The list handler uses the complete merged backend per DB roster name so the advertised policy matches what creation enforces.
- **Freeform backends** (pi, opencode, agy, cursor, omp, custom) without configured lists accept any ID and report `allowed_models` empty; they never produce a 422.

- **Schedule saves** use the same explicit model policy against a non-reserving backend selection from live merged config. REST and MCP reject before persistence. LastRunAt bookkeeping stays unchanged.
- **Supervisor manifest** includes `internal/agent/agent.go`, not `internal/agent/create.go`. Validation runs outside supervisor spawning, so no protocol or surface version bump is required.
