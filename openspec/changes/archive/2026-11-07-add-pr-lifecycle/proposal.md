## Why

`pr_state` collapses a merged PR and a closed-unmerged PR into `merged-closed`, so clients render abandoned work like shipped work. ENG-1032.

## What changes

- The PR poller persists `lifecycle` (`open|merged|closed|unknown`) beside `state`/`url` in `task_meta` namespace `pr`, from the authoritative GitHub result.
- `GET /api/tasks` and `GET /api/tasks/{id}` add `pr_lifecycle`. `pr_state` is unchanged.
- A changed PR cache commit publishes `task.pr_changed` with `{"url","state","lifecycle"}`.
- A terminal cached entry without a verified merged or closed lifecycle gets one immediate attempt per daemon process, then dormancy-cadenced retries if unsuccessful, and is skipped permanently once verified.

## Capabilities

### Modified capabilities

- `pr-status`: lifecycle persistence, terminal-skip refresh, change event.
- `rest-api`: `pr_lifecycle` on the task DTO.
- `events-bus`: `task.pr_changed`.

## Non-Goals

- No web, TUI or macOS change. Web consumption is the follow-up ENG-1073 (Web: Tell closed PRs apart from merged PRs).
- New merge transitions retain both notifications. Lifecycle backfill of an already terminal cache sends neither notice. Cleanup eligibility still uses raw MERGED evidence.
