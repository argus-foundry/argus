# Design

- A refcounted per-task mutex (`taskLocker`, `internal/api/tasklock.go`) is shared by archive/unarchive, input, resume and restart. The archive handlers stay on `db.Update` (SetArchived wipes task_meta).
- Input holds the lock across the write, so archive waits for in-flight writes and later input sees `Archived`.
- Other REST errors on touched routes gain a `code` field; the existing 404 message on unknown task input changes from "no active session" to "task not found" per the ticket.
