## Context

`resolveTaskChoice` treats a non-empty `account` as a pin: every Claude tier is scored using that account's reading (`choice.go`). The three frontends always send a non-empty account once more than one account exists. `AccountRouted` is stored only when routing picked a non-blank account, so an implicit default is indistinguishable from a legacy blank, and MCP `task_create` pins blank callers to `default`.

## Goals / Non-Goals

**Goals:** unpinned tasks/workers route by headroom; bursts spread; deterministic and testable.

**Non-Goals:** per-account Codex probes; threshold normalization between Claude and Codex; migrating existing rows; persisting the spawn ledger across daemon restarts.

## Decisions

1. **Auto = blank on the wire.** No new sentinel is stored. `"auto"` is accepted at REST/MCP boundaries as an alias and normalized to blank. Project `project_accounts` pins still apply to blank (existing behavior), so Auto means "no deliberate choice", not "ignore project config".
2. **Blank is always implicit.** Explicit default is already stored as `"default"` (`resolveTaskAccount`), so a blank stored account means no deliberate choice. `AccountRouted` is set to true for every task created without an explicit/project account, including when routing resolves to `default` (stored blank). Inheritance paths (MCP, hera, fork) treat blank OR `AccountRouted` as routable; a non-blank, non-routed account is a pin. Legacy `default`-pinned rows stay pinned.
3. **Burst ledger lives in `backendtier`**, in-process, keyed by (backend, account-with-""→default). `ResolveAndReserve(cfg, backend, account)` holds a mutex across resolve + record so concurrent spawns see each other. `ResolveChoice` stays pure (previews/backend-only callers don't reserve). Effective usage = reading + `5 × spawnsInWindow(30m)`; used for threshold checks and headroom scoring. Among unknown-reading tiers the one with fewest recent spawns wins (list order breaks ties). Clock is injectable.
   - *Alternative:* count tasks from the DB across processes. Rejected: ResolveChoice is config-only today, and bursts come from the daemon (hera/MCP/schedules), where one process sees them all. TUI-process spawns are human-paced; limitation noted.
   - *Alternative:* count only spawns since the reading's probe time. Rejected: readings expose only a percentage; a fixed 30-min window (= probe TTL) slightly over-penalizes, which only spreads more.
4. **Forks** inherit a routable account when the source was routed/blank; `PinnedAccount` remains for pinned sources.
5. **Codex threshold skew**: out of scope (see proposal). **Existing pinned rows**: no migration code.

## Risks / Trade-offs

- Penalty constants are not configurable → conservative constants; revisit if misfires.
- Ledger resets on daemon restart → usage probe at startup bounds the error.
