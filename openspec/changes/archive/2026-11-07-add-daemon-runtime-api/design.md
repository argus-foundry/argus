## Decisions

- api cannot import skew/daemon (daemon imports api), so the api defines `RuntimeSource`; `internal/skew.RuntimeSource` implements it over `Daemon.BootInfo` and is wired in `cmd/argus` before Serve.
- `boot_id` is a random per-process value created in `api.New`, so a successor always differs and a retried request against it conflicts.
- Installed hash is the SHA-256 of the resolved `os.Executable()`, cached by path+size+mtime so polling does not re-read the binary.
- Supervisor `state` is hash equality; `compatibility` is the surface verdict, deliberately independent (a hash-stale supervisor can be compatible, and restarting it would SIGHUP agents).
- `restart_supported` is true only when a supervisor is present, because legacy in-process mode would kill agents. Acceptance failure (500) is a preflight (test-binary guard) before the 202.
- The restart reuses `spawnSuccessorDaemon` with the self-update 500ms flush delay; it never builds or touches the supervisor.

Restart retains the child wait handle until exit. An early exit clears the pending latch and logs through slog, allowing retries without overlapping live successors.
