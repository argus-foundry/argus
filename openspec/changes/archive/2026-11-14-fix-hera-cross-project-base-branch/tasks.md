## 1. Regression coverage

- [x] 1.1 Add failing gater tests for root, blocker, explicit invalid base, subcoord, and same-project stacking.
- [x] 1.2 Add agent tests for repository branch resolution and default versus explicit spawn-worker input.

## 2. Implementation

- [x] 2.1 Validate derived bases with target config and source-task project before both materialization paths; log fallback.
- [x] 2.2 Update MCP descriptions and gotcha documentation.

## 3. Validation and delivery

- [x] 3.1 Run focused tests, review the diff, archive specs, and run make pre-pr.
- [ ] 3.2 Commit, push through Iris, open PR against master, and report green CI to coordinator.

Local build, vet, formatting, lint, vulnerability scan, focused regression tests, and gater race tests passed. Full local gates encountered shared-host timeouts in untouched packages; the coordinator explicitly authorized PR delivery with the full suite deferred to CI. A serial local run is also in progress.
