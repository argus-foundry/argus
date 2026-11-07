## Why

A task's PR is found only from its branch, and polling rewrites the cache, so a user cannot attach a PR that differs from the branch lookup. ENG-1042.

## What changes

- New `task_pr_links` table holds manual association intent, separate from the derived `pr` cache in `task_meta`.
- `GET|PUT|DELETE /api/tasks/{id}/pr` read, set and clear the association. PUT validates and canonicalizes a github.com pull request URL; DELETE restores branch discovery.
- The branch poller skips linked tasks and its write refuses them; a manual poll pass verifies the linked PR by URL through `gh`.
- `task.pr_changed` gains `source` (`manual|automatic`), the same payload struct the poller already uses.

## Capabilities

### Modified capabilities

- `pr-status`: manual association, verification, poller exclusion.
- `rest-api`: `/api/tasks/{id}/pr`.
- `events-bus`: `source` on `task.pr_changed`.

## Non-Goals

- No web, TUI or macOS change. Follow-ups: ENG-1083 (Web: Set or clear a task's PR link) and ENG-1094 (TUI: Set or clear the selected task's PR link).
- No GitHub mutation, and no outbound HTTP from the supplied URL: only `gh pr view` on the canonical URL.
