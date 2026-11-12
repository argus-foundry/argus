# Worktree cleanup ownership and path operands

Keep failed cleanup with the process watch budget so retired or degraded entries retain a live owner and can reclaim capacity after retry. Pre-close removal errors remain retryable; uncertain irreversible backend Close errors remain conservatively charged.

Treat ordinary filenames as whole operands, including literal arrows. Separate rename and copy pairs only outside Git quoting. Preserve the existing fingerprint cap and frontend follow-ups.

Coordinator message 3277 approves F2, F3 and the documented concurrent kqueue teardown limitation. Keep fsnotify 1.9.0 with no timing wrapper or backend workaround. Fold and archive in the single round 3 commit.
