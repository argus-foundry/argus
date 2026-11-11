## Context

Installed Claude Code 2.1.317's wi() config writer acquires proper-lockfile's mkdir-based `<config>.lock`, then reads fresh and writes. Its default stale lease is ten seconds with a five-second update interval. The wrapper uses realpath resolution for the config target, but the explicitly supplied lock path is the original config spelling plus `.lock`.

## Goals and non-goals

Prevent Argus from overwriting a concurrent Claude update, while preserving bounded fail-open creation. Keep per-worktree keys; do not broaden trust to main repositories or implement Codex trust in this correction.

## Decisions

- Acquire the same lock directory with mkdir, waiting at most 500ms. Never steal an existing lock, even one with an old timestamp; Claude can recover its own stale locks.
- Replace the process-local mutex with this per-config cross-process lock. Refresh the owned directory inode every second, below Claude's stale/update thresholds.
- Hold an open directory descriptor and compare inode identities before publishing and releasing. Refresh the descriptor rather than following a replacement path. Abort before rename if the lease was lost; never remove a successor's lock.
- Preserve the existing atomic writer behavior for other callers; add a pre-rename guard for trust's lease check.
- PTY probes used temporary HOME/config dirs, a throwaway linked worktree, synthetic local-only credentials, and no model request. Claude: no entry and parent entry prompt; exact worktree and main repo do not. Codex 0.167.0 with --no-daemon: parent entry, bypass flags, and a per-launch `-c projects."<worktree>".trust_level="trusted"` still prompt; exact worktree and main repo entries in the user config do not. Claude XI/iy/sy stops ancestor walking at the worktree Git root. A same-account matching main-repo trust entry suppresses the dialog; trust in a different account/path does not. Codex trust provisioning remains the named follow-up because the approved per-launch experiment failed.

## Risks / Trade-offs

- A contended or abandoned lock may leave the task at Claude's trust dialog; fail open and log rather than risking another session's data.
- Claude can change its private protocol in future releases; retain the verified version and probe results in gotchas.
