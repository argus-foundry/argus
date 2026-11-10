- [x] Reproduce large native metadata rejected at c15c56a9.
- [x] Add a move-specific 4 MiB budget while preserving the default 64 KiB reader.
- [x] Verify large records copy completely, over-budget records fail, and existing security tests remain green.
- [x] Archive the delta and base spec with one narrow commit.

Required post-commit validation is one locked focused race invocation for agent, api and the changed codexctx package, followed by one full make pre-pr on the committed tree. Record the exact clean HEAD at gate start and end under .context/pr1154/r4/; do not amend after gate start.
