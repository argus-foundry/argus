## Why

Follow-ups to the daemon-restart false-complete fix. (1) The status probe itself still lied after a restart: the old daemon's teardown detaches its supervisor client, which closes every cached session handle, and the daemon keeps serving the TUI's open connection, so it answered "not alive" (with the cached PID) for sessions the supervisor still ran. (2) The reaper now spares sessions of tasks with a live coordinator binding, so an accepted sub-coordinator's idle session lingers.

## What Changes

- In supervisor mode, a cached handle that reads dead is never trusted: the daemon asks the supervisor, reports its answer, and answers an error ("unknown") when it cannot be asked.
- Accepting a role whose task also holds a coordinator binding (a sub-coordinator) ends that coordinator binding (reason accepted). Worker and freelance bindings are untouched.
- Reviving or resuming the task (hera_revive, hera_send's dead auto-revive, TUI resume including the third-Enter revive, REST resume and restart) re-opens the accepted coordinator binding, keeping the same binding row. A failure to restore is reported, not swallowed.

## Non-Goals

No supervisor behavior change: the supervisor's own runner is never a supervisor client, so the new probe branch is inert there; the stream surface version is unchanged and only its digest is re-recorded. Accepting a sub-coordinator with live child workers leaves those workers without a live coordinator by design of accept. No new macOS/web surface: REST resume/restart already share the restore.
