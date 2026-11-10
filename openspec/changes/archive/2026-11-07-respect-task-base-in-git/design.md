## Decisions
- `Options` struct instead of a bare string so later git-lane tickets add fields without another signature change.
- Requested base tries the local ref, then `origin/<base>`; values starting with `-` or containing whitespace are rejected as non-refs and verified with `rev-parse --verify --end-of-options`.
- `resolved_base` for the fallback is the ref name that produced the merge-base (`origin/master`, the upstream's abbreviated name, ...).
- `Err` is `json:"-"`: only `git status` failing (or a base-diff failing once a base resolved) is an error; the TUI ignores it as before.
- Deviation: the `--no-index` untracked fallback is now gated on the file being untracked; previously any unchanged tracked file produced a synthetic added diff, which showed parent-task files as the child's own.
