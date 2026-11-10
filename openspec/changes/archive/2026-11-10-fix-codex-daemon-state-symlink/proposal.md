## Why

Argus mirrors the source Codex home into an isolated `CODEX_HOME` by symlinking every root entry except its managed files. Codex 0.166.0 refuses to start when `app-server-daemon` in that overlay is a symlink, even when it points to a directory. Existing overlays therefore break new Codex tasks until repaired.

## What Changes

- Give each Argus Codex overlay its own real `app-server-daemon` directory. Do not link the source home's daemon sockets or process state.
- On a later launch, replace an existing overlay symlink at that path with the local directory. Preserve any non-symlink overlay content rather than deleting it.
- Keep the existing source-home links for configuration, authentication, sessions, and user skills.
- Cover default and account-specific overlays with regression tests, including an already-created symlink.

## Impact

Codex tasks started by the daemon, TUI, web, and macOS use the same Argus launch path; no client wire change is needed.
