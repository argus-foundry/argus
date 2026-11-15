## 1. Recovery and retention

- [x] 1.1 Add regression tests for invalid recurrence, orphaned messages, unread coalescing, events, retention and explicit zones.
- [x] 1.2 Implement per-timer isolation, durable submitted receipts and safe terminal-row pruning.
- [x] 1.3 Keep delivery/cancellation retries independent from firing failures and log state errors.

## 2. Review corrections

- [x] 2.1 Correct singular display and isolate daemon-only state types with an honest digest.
- [x] 2.2 Count notifier registrations and test the daemon's actual output through the real notifier.
- [x] 2.3 Remove batch tooling and CI changes from the PR while retaining local verification capability.

## 3. Verification

- [x] 3.1 Merge current master, run targeted and approved gate verification, and archive this change.
- [x] 3.2 Update the existing PR without rewriting stacked history, watch CI and report completion.
