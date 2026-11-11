## Context

The resident already delivers a self-contained inline default-mode policy. Only shipped defaults change; flag stripping, project setting sources, merged user ask/deny, sandbox carry, Stop hook and skills remain intact.

## Goals / non-goals

- Allow ordinary work and reviewed connector reads/drafts; keep outward actions behind confirmation.
- Preserve Settings/config.toml replacement and override markers, and resident-coordinator structural restrictions.
- No frontend or REST contract changes; TUI, web and macOS receive identical daemon behavior.

## Decisions

Claude Code documents deny > ask > allow regardless of specificity. `mcp__server` and `mcp__server__*` cover one server; only allow globs after a literal server prefix work. Global `*`/`mcp__*` allow rules are ignored. Source: https://code.claude.com/docs/en/permissions (checked 2026-11-10).

Use explicit built-in names, a broad Argus allow beneath the existing structural Hera ask/deny, reviewed concrete connector reads, and the live resident transcript’s reviewed tool catalog. Keep explicit write ask rules, including read-looking mutations, and Slack send denies. Reject whole external-server grants because unknown writes would be approved. Argus cannot classify all unknown connectors at launch: Claude discovers hosted connectors internally after spawn. A runtime name-classifying hook cannot prove read-only semantics either and is unnecessary for a shipped-default-only change.

## Risks / trade-offs

- Mixed/ambiguous tools (for example Claude Docs `batch`, Figma `use_figma`, Sketch `blueprint_comments`, artifact `db_query`) ask as whole tools.
- External allow rules are concrete: unknown tools, including new read tools and read-looking writes, still prompt. Refresh reviewed catalogs as connectors change.
- Built-in Bash can perform outward work through CLI/network APIs. Connector rules do not inspect Bash intent; the Argus sandbox still bounds its access.
- Existing unattended resident workers run bypass and deny only explicit resident outward tiers. Unknown tools absent from those tiers can run there; this pre-existing gap is not solved by widening defaults.

## Migration plan

Build/install the new Argus binary, restart the session-supervisor (it owns Runner.Start/BuildCmd with supervisor mode enabled), restart the daemon for its config defaults, then restart the resident to rebuild inline settings. A daemon-only bounce reattaches the old resident and does not update policy. Supervisor restart interrupts all owned agent sessions; coordinate it with Aaron. With supervisor disabled only daemon plus resident restart is needed.

Read-only inspection found no resident block in ~/.argus/config.toml and only resident.name, resident.enabled, resident.memory_dir in the DB: no stored permission overrides mask these defaults.

Exact host deployment, after merge and once agent interruption is acceptable:

```sh
make install-signed
argus session-supervisor stop
argus daemon restart
```

`daemon restart` runs the replacement daemon in that terminal and auto-starts the updated supervisor. The supervisor stop ends all owned sessions, including the resident; the enabled resident ensure-loop then starts a replacement with the new policy. Verify `argus doctor` reports spawn surface 46 or newer. Do not run these during this worker task.

## Validation

`GOFLAGS=-p=1 make pre-pr` passed with the installed Go tools added to PATH; no timeout override or skipped test was needed. All 741 workspace tests passed. Focused generated-policy and catalog tests passed; final formatting/lint rechecks passed after strengthening the inventory assertion. Config coverage is 95.7%. Existing policy construction already logs via uxlog and slog, so no logging path was added.

`openspec validate widen-resident-allow-tier --strict` passed. Repository-wide strict validation reports two pre-existing untouched spec failures: desktop-web-workspace requirements 5/20 lack SHALL/MUST, and rest-api requirement 53 lacks a scenario.
