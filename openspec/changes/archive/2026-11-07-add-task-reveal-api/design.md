## Decisions

- Containment is checked on the symlink-resolved path immediately before launch, with a lexical pre-screen (absolute, `..`, NUL, `.git`), so a symlink swapped after validation cannot widen scope beyond the validate-to-launch window.
- Unsupported host is reported after path validation so invalid input is never ambiguous; the launcher and GOOS are struct-field seams so tests never run `open`.
- Directories are rejected in file mode (`invalid_path`); revealing a directory is the worktree mode only.
- Master-only via `requireMaster`; deviation from nothing in the ticket.

Worktree mode rejects final symlinks and non-directories with Lstat and selects Finder explicitly. File-mode validation retains its documented validation-to-launch pathname window.
