## 1. Filter

- [x] 1.1 Byte matcher + `syncHoldLen` (open-block start, trailing partial prefix)
- [x] 1.2 `SyncTracker` for the pump's writer (self-releasing after the timeout)

## 2. Pane

- [x] 2.1 `syncGate` in the live incremental feed: withhold, timeout, byte cap, rate-limited uxlog
- [x] 2.2 Reset on ResetVT / rebind / in-place reset / ring-wrap catch-up
- [x] 2.3 Replay the painted cache when everything new is withheld

## 3. Pump

- [x] 3.1 Writer skips wakes wholly inside a block; `wakeAfter` timer for the hold's expiry

## 4. Proof and docs

- [x] 4.1 Unit tests (split sequences, nested/duplicate, timeout, reset on rebind)
- [x] 4.2 Real-bytes fixture from a Claude session log; offline before/after measurement
- [x] 4.3 Gotcha entry + knowledge index
