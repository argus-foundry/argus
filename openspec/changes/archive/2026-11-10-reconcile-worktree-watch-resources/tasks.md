# Tasks

- [x] Reproduce the reviewers' Add-gap and repeated kqueue teardown failures with real backend tests.
- [x] Reconcile reservations after Add and on cached refresh, and remove watches before close.
- [x] Prove normal, degraded and partial-Add teardown releases actual descriptors and failed teardown retains reservations.
- [x] Kill each reverted fix with its own regression.
- [x] Run locked focused race and full pre-pr gates.
- [x] Fold and archive the modified requirement before committing.
