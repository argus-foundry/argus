## Why
The web Changes list fetches `/git/diff` once per visible file to show added and removed lines, spawning many git subprocesses and delaying counts.

## What Changes
- `gitutil.FetchGitStatus` also returns `files`: `{path, original_path?, status, additions, deletions, kind}` per changed path, computed in a fixed number of git commands (status `-z`, `diff HEAD --numstat -z`, and when a base resolves `diff --name-status -z` plus `diff --numstat -z` against the merge-base) plus in-process line counting for untracked files.
- `GET /api/tasks/{id}/git/status` returns `files` from the same cached snapshot, so counts share the comparison base and `git_revision` of the legacy fields.

## Non-Goals
- No web, macOS or TUI changes. Follow-up: ENG-1105 (Web: Read line counts from git status).
- No new event or route.
