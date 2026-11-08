- [x] Scope directed by em in Hera message 1319.
- [x] Add failing slow-account, burst, overlap and legacy-metadata tests.
- [x] Separate measurement and usage read ownership with task/session fences.
- [x] Verify workspace/browser tests and full pre-pr, archive and commit locally.

Full make pre-pr passed with Go 1.26.10 and GOFLAGS=-p=3. Workspace tests passed 613 cases and the expanded browser suite passed 27 cases. Earlier unchanged Go and browser timing failures are recorded in the local validation logs.

Selected session.started and session.exited regression tests pass. Removing session.exited in a scratch production copy fails the new exit test, addressing QA Hera message 1391.
