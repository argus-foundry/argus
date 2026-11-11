## 1. Spec approval

- [x] 1.1 Aaron approves the design via Plannotator.

## 2. Transcript state classifier

- [x] 2.1 Tests, then `internal/transcriptstate`: bounded-tail Claude JSONL classification (parked_question with question and options, mid_turn, idle, unknown), including the real `coord-superfans` shape.

## 3. Ledger and planner

- [x] 3.1 Tests, then `[bounce_recovery]` config (enabled, max_concurrent, stagger_seconds, expiry_hours) with defaults.
- [x] 3.2 Tests, then ledger read/write over task_meta namespace `bounce`, including bounce_id overwrite and attempts cap.
- [x] 3.3 Tests, then planner: role kind, coordinator-above lookup, delegated vs unsupervised, dormant idle, depth ordering, coordinator-unresumable fallback, residents/archived excluded.

## 4. Note builder

- [x] 4.1 Tests, then `[argus] ` note builder (worker, coordinator-with-team, parked question, sanitization and caps).

## 5. Recovery worker and daemon wiring

- [x] 5.1 Tests, then worker: throttle, settle detection, retry/backoff, expiry, pre-action re-check, superseded skip, slog `[bounce-recovery]` logs on every path.
- [x] 5.2 Hook the ledger write into `reattachSupervised` (after RefreshResumeSessionID, nil-live-set skip, off-switch) and start the worker after the daemon is serving.
- [ ] 5.3 Verify against a copy of the `coord-superfans` transcript how Claude resumes a dangling AskUserQuestion; adjust note wording if needed.

## 6. Docs and delivery

- [x] 6.1 Gotchas in `daemon-rpc.md`/`orchestration.md` and index row; README config reference row.
- [ ] 6.2 Announce, then run `make pre-pr` once. Commit, open PR with iris, archive the change in the PR, report to coord.
