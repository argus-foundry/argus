## Strict mutation validation

- [x] Reproduce zero timestamps and trailing malformed/oversized bodies with failing HTTP and DB tests.
- [x] Reject zero expected timestamps in API and strict store methods, preserving legacy callers.
- [x] Require EOF through each bounded reader before mutation.
- [x] Run focused DB/API race tests, strict specs and one staggered full make pre-pr.
- [x] Sync base specs, archive and commit locally.
