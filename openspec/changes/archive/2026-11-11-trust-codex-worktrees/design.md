## Context

Temporary-config PTY controls on Codex 0.167.0 show no-entry, parent-only entry, bypass-only and quoted dotted-key override all prompt. An exact worktree or main-repository entry in the user config works. A single `-c 'projects={"<physical worktree>"={trust_level="trusted"}}'` also works and reaches the YOLO composer with bypass enabled. The whole table avoids dotted-key splitting inside quoted paths.

## Goals and non-goals

Trust only the current Argus worktree in the launched Codex process without editing any account config. Retain Claude's account-specific persisted trust and compatible lock. Do not add trust provisioning for unrelated backend families or broaden trust to main repositories.

## Decisions

- Declare a dynamic WorktreeTrustFlag hook on Family, implemented by Codex. BuildCmd invokes registry hooks rather than branching on backend names.
- Append this flag after resume command reconstruction and before prompt delivery, so fresh and resumed launches both receive it.
- Canonicalize symlinks and make the path absolute, preserving the physical path spelling Codex receives. Encode the entire path as a JSON string (a TOML basic-string-compatible key), then shell-quote the complete inline-table assignment as one argv value. Slashes, dots, quotes, dollar signs, backticks and apostrophes must remain data.
- Use `[agents] trust_worktrees` for both families, default true. Remove the unmerged Claude-only spelling; no compatibility alias is needed.
- Keep Codex's trust map process-scoped. Real config/read shows the inline table merges with existing trusted and untrusted project entries instead of replacing them. Account SHA-256 and mtime remain unchanged through app-server startup/read; an explicit config/value/write changing model_reasoning_effort follows the shared symlink but saves only that setting, never the injected trust entry. Real TUI startup can save screen-reader state and increment its model-availability counter, changing the hash/mtime, but the diff contains no injected project trust entry and the shared symlink remains intact.
- Add the new launch helper to the supervisor spawn manifest and bump the observable spawn surface because an older supervisor cannot inject the override.

## Risks / Trade-offs

- A missing or unreadable worktree cannot be canonicalized; log [trust] and continue normal launch, which may show the dialog.
- CLI override parsing can change between Codex versions; retain the verified inline-table form and regression tests instead of simplifying it to a dotted key.
