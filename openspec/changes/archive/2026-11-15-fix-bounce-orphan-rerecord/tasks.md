## 1. Approval and regression

- [x] 1.1 Obtain coordinator approval of root cause and evidence rule before implementation.
- [x] 1.2 Add failing regressions for repeated daemon starts with dead bound workers, unchanged ledger bookkeeping and no duplicate ARGUS_BOUNCED, including dormant team recording.
- [x] 1.3 Cover snapshot-backed stale statuses, same-event repeat reconciliation, genuine later losses, and uncertainty retaining snapshot evidence.

## 2. Implementation

- [x] 2.1 Filter recovery candidates by session-loss evidence before recording and signaling; preserve historical ledger entries and add skip logs.
- [x] 2.2 Prevent dormant team recording from creating or overwriting entries without current session-loss evidence.
- [x] 2.3 Update daemon-rpc gotchas and review all frontend implications.

## 3. Delivery

- [x] 3.1 Run focused tests, review correctness and adversarial test coverage, and pass make pre-pr.
- [x] 3.2 Validate the change's specs and confirm the implementation is ready for archiving.

After archiving, commit and push using iris, open the PR through iris_gh_pr_create, watch CI until green, and report its URL without merging. These delivery steps are tracked by the Hera worker rather than the archived implementation checklist.
