## Read-state compare and set

- [x] Reproduce edit, resolution and deletion races using an independent connection before fixing production code.
- [x] Check affected rows and classify a lost comparison without emitting success.
- [x] Audit legacy read, material update and transactional dismissal writes.
- [x] Add timestamp-skew, name/tab sanitization, kind-only event and UTF-8 note-byte tests; kill all five QA survivors.
- [x] Run focused race tests and full make pre-pr, sync specs, archive and commit locally.
