## 1. Research and specification

- [x] 1.1 Merge current master and verify official Claude Code behavior with a guide sub-agent.
- [x] 1.2 Specify launch, fallback, fixed-rule policy and waiting behavior; record config-knob decision.

## 2. Implementation

- [x] 2.1 Add failing launch, policy, fallback logging and needs-input tests.
- [x] 2.2 Force auto, retain inline policy and replace shipped blanket Bash allow with narrow read-only grants.
- [x] 2.3 Log native fallback notices and detect terminal missing-verdict stops without flagging transient blocks, including with a live bridge.

## 3. Validation and delivery

- [x] 3.1 Update gotchas, evaluate frontend parity and record supervisor surfaces/digests.
- [x] 3.2 Run targeted tests, full gate, approved exhaustive package batches and coverage floor; review code and test strength.
- [x] 3.3 Archive the change on the implementation branch; record required restarts and the outward-action recommendation in PR materials. Publication and authoritative CI are tracked by the Hera worker role.
