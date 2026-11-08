## Context

`resolveTaskChoice` treats a non-empty `account` as a pin: every Claude tier is scored using that account's reading (`choice.go`). The three frontends always send a non-empty account once more than one account exists. `AccountRouted` is stored only when routing picked a non-blank account, so an implicit default is indistinguishable from a legacy blank, and MCP `task_create` pins blank callers to `default`.

## Goals / Non-Goals

**Goals:** unpinned tasks/workers route by headroom; bursts spread; deterministic and testable.

**Non-Goals:** per-account Codex probes; threshold normalization between Claude and Codex; migrating existing rows; persisting the spawn ledger across daemon restarts.

## Decisions

1. **Form `default` = blank on the wire.** No Auto entry and no new sentinel. The three forms map the `default` selection to a blank account; named accounts submit their name. REST/MCP keep `account: "default"` as a deliberate pin (programmatic callers can omit `account` to route). Project `project_accounts` pins still apply to blank (existing behavior). Trade-off: the forms cannot pin the default account; only a REST/MCP creation with `account: "default"` can (a `task_move_account default` stores a blank account, which is unpinned).
2. **Blank is always implicit.** Explicit default is already stored as `"default"` (`resolveTaskAccount`), so a blank stored account means no deliberate choice. `AccountRouted` is set to true for every task created without an explicit/project account, including when routing resolves to `default` (stored blank). Inheritance paths (MCP, hera, fork) treat blank OR `AccountRouted` as routable; a non-blank, non-routed account is a pin. Legacy `default`-pinned rows stay pinned.
3. **Burst ledger lives in `backendtier`**, in-process, keyed by (backend, account-with-""→default). `WithReservation(resolve)` holds a mutex across resolve + record so concurrent spawns see each other. `ResolveChoice` stays pure (previews/backend-only callers don't reserve). Effective usage = reading + `5 × spawnsInWindow(30m)`; used for threshold checks and headroom scoring. Among unknown-reading tiers the one with fewest recent spawns wins (list order breaks ties). Clock is injectable.
   - *Alternative:* count tasks from the DB across processes. Rejected: ResolveChoice is config-only today, and bursts come from the daemon (hera/MCP/schedules), where one process sees them all. TUI-process spawns are human-paced; limitation noted.
   - *Superseded during implementation:* the penalty is a placeholder that clears per spawn once a reading taken at least 5 minutes (`SpawnSettle`) after that spawn arrives (readings carry `LastProbedAt`); the 30-min window remains only as the backstop for untimestamped readings. A fixed window alone let one fresh reading seconds after a spawn wipe the placeholder mid-burst on dogfood.
4. **Forks** inherit a routable account when the source was routed/blank; `PinnedAccount` is replaced by `InheritableAccount`, the single inheritance rule for forks, MCP and hera.
5. **Unpinned resolution order** (Aaron's input, Option B): project account → backend-restricted `ResolveChoice` (today's behavior) → step 3, unrestricted `ResolveChoice` adopting backend+account together. Step 3 runs only when no backend was explicit and the project backend produced nothing; explicit backends never change. Coordinators always arrive with an explicit Claude backend (`resolveCoordinatorBackend`), so they are structurally Claude-only.
6. **Codex threshold skew**: out of scope (see proposal). **Existing pinned rows**: no migration code.

## Risks / Trade-offs

- Penalty constants are not configurable → conservative constants; revisit if misfires.
- Ledger resets on daemon restart → usage probe at startup bounds the error.
