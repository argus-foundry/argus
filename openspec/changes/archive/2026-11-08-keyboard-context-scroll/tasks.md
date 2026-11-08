- [x] Scope directed by em in Hera message 1317.
- [x] Reproduce missing keyboard access with a browser regression test.
- [x] Make the named panel focusable with native autofocus and verify keyboard scroll, refresh, Tab exit and Escape.
- [x] Run workspace tests, browser tests and full pre-pr, archive and commit locally.

Full make pre-pr passed with Go 1.26.10 and GOFLAGS=-p=3. Workspace tests passed 613 cases and the expanded browser suite passed 27 cases. Earlier unchanged Go and browser timing failures are recorded in the local validation logs.
