## Decisions

- **Trust at every resident launch, not at creation.** The resident has no creation step and its account changes through automatic switches, manual moves and restarts, all of which relaunch through BuildCmd. A launch-time write is cheap here: one session, infrequent restarts, and the trust write is a no-op read when the entry already exists.
- **Only the resident.** Ordinary tasks keep creation-time trust (previous change); per-launch writes for the whole fleet were rejected.
- **Reuse the Claude trust writer and account path resolution.** The config path logic is shared with worktree trust so the resident writes the same `.claude.json` its launch reads.
- **Fail open.** Each directory is trusted independently; failures log `[trust]` and the launch continues.
