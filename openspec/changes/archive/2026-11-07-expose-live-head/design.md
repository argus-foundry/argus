## Decisions
- `head` is part of `GitStatusRefreshMsg`, so it rides the cached snapshot and `git_revision` moves on checkout; the existing HEAD watcher emits `worktree.changed`.
- Deviation: the daemon PR poller cannot reach the API server's snapshot cache, so it calls the same `gitutil.ReadHead` helper (two cheap git reads per eligible task) through a `prLiveHead` seam instead of the cache.
- Unborn HEAD is `branch` set with `oid` null; detached HEAD is `branch` null, `detached` true. The poller looks up neither detached nor unreadable HEADs and keeps cached PR state.
- A stored name is never substituted for an unavailable live HEAD.
