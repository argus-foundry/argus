# Design

## Decisions
- Path validation (`gitutil.ValidateWorktreePath`) rejects empty, absolute, non-normalized, `..`, any `.git` component, directories, and parents or leaf symlinks resolving outside the worktree; it runs again immediately before mutation inside `DiscardFile`.
- Git runs with `GIT_LITERAL_PATHSPECS=1` and `--`; no caller text reaches a shell.
- Rename detection scans the whole-tree porcelain output so that either side of a pair is refused (limiting by pathspec hides the pair).
- Discards for one task serialize on the watcher entry's mutate lock.
- If the post-discard refresh fails the event is still sent without a revision, which clients treat as an invalidation.
- Error bodies for this route and the 503 path carry `code`; legacy routes keep `{"error"}` only.
