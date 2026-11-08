## Tasks

- [x] Add failing workspace tests for the stale working tag and the focus-out report.
- [x] Gate the working tag on task liveness and drop focus-out reports at terminal input.
- [x] Verify workspace tests and document gotchas.
- [x] Archive into desktop-web-workspace before the commit.
- [x] Scope focus-out suppression by backend family, cover navigation caller and missing-task rows, and reconcile browser assertions and spec wording.
- [x] Prefetch backend classification outside the input queue, match the real empty custom kind, and test pending reads plus shared-cache task-switch cancellation.
- [x] Hold unresolved focus-out for at most two seconds, discard stale/teardown reports, retry failed prefetch with capped backoff, and cover real cold attach plus fulfillment-time cancellation.
