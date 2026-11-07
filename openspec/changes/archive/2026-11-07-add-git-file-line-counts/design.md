## Decisions
- Per-file scope mirrors `FetchFileDiff`: a path present in `diff HEAD --numstat` uses those counts, otherwise the merge-base diff, otherwise untracked content.
- `files` is part of the status message, so `git_revision` (hash of the message) changes when counts change and the cache needs no extra keying.
- `kind` is `text`, `binary`, `directory` (untracked directory entries collapsed by git status, path keeps the trailing `/`) or `unavailable`. Counts are null unless `text`; a failing numstat command or unmerged path yields `unavailable`, never zero.
- Untracked files are counted in-process (newline count, NUL in the first 8000 bytes means binary, symlink counts as one added line, files over 8 MiB are `unavailable`), avoiding a per-file `git diff --no-index`.
- Tracked paths absent from both numstat scopes without an error report 0/0 text, matching an empty `/git/diff`.
- The snapshot is assembled from several git commands, not one atomic read; a concurrent write is corrected by the next watcher refresh.
- Status codes are collapsed to one letter (`M`, `A`, `D`, `R`, `U`, `??`), preferring the worktree column.
