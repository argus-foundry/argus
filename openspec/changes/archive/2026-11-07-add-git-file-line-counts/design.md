## Decisions
- Per-file scope mirrors `FetchFileDiff`: a path present in `diff HEAD --numstat --no-renames` uses those counts, otherwise the merge-base diff, otherwise untracked content.
- `files` is part of the status message, so `git_revision` (hash of the message) changes when counts change and the cache needs no extra keying.
- `kind` is `text`, `binary`, `directory` (untracked directory entries collapsed by git status, path keeps the trailing `/`) or `unavailable`. Counts are null unless `text`; a failing numstat command or unmerged path yields `unavailable`, never zero.
- One batched `check-attr --stdin -z` call and one textconv configuration query classify count reliability. Git numstat ignores textconv even with `--textconv`, so configured textconv paths are `unavailable`; untracked paths with conversion-sensitive attributes or unsupported named diff drivers are also `unavailable`. Forced binary is `binary`, and forced text disables NUL sniffing.
- Untracked files are counted in-process (newline count, NUL in the first 8000 bytes means binary, symlink counts as one added line, files over 8 MiB are `unavailable`), avoiding a per-file `git diff --no-index`. Reads use an `os.Root`, nonblocking no-follow opens, descriptor validation, an 8000-byte binary prefix and a bounded streaming reader; growth beyond the cap is unavailable.
- Tracked paths absent from both numstat scopes without an error report 0/0 text, matching an empty `/git/diff`.
- The snapshot is assembled from several git commands, not one atomic read; a concurrent write is corrected by the next watcher refresh.
- Status codes are collapsed to one letter (`M`, `A`, `D`, `R`, `U`, `??`), preferring the worktree column.

- Rename counts use destination-only semantics with `--no-renames`, while status/name-status preserve rename identity. Local status duplicates retain the first tracked entry, matching a staged deletion before a recreated untracked path.
- Review correction: the original archived wording promised blanket text-count equivalence and blanket 500 failures. The base and archive now state reliable counts or unavailable, destination-only rename counts, and warm-cache 200/stale versus cold 500/503.
