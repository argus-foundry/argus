## Context

Argus already seeds `projects[<worktree>].hasTrustDialogAccepted` in the task account's `.claude.json` at creation. Claude 2.1.326 added a trust dialog for `--add-dir` directories. A real PTY probe showed the dialog for `~/.argus/skills` disappears once `projects["/Users/<u>/.argus/skills"].hasTrustDialogAccepted = true` is persisted.

## Decisions

- **Seed at creation, inside the existing Claude trust hook.** Reuses the lock, account resolution and off switch. Seeding on every launch was rejected: it would write account config from the supervisor on every start.
- **Materialize the skills workspace first** via the existing `ensureSessionSkillsFn` seam, because trust canonicalizes the path with `EvalSymlinks` and the directory must exist. An empty root (test binaries) skips skills trust.
- **Independent failures.** Worktree and skills trust are attempted separately; errors are joined and logged once by the caller, and creation continues.
