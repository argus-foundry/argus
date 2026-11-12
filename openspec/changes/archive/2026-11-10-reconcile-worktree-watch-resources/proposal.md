# Reconcile worktree watcher resources

Close the reservation gap after fsnotify Add and reconcile drift during cached status refreshes. Remove watched paths before closing fsnotify so kqueue file descriptors actually close before their reservations become reusable. Failed teardown retains capacity conservatively.

The round 2 coordinator brief approves this correction to the existing watcher resource invariant. No vendor patch, replacement backend, API shape or frontend change. Existing named parity follow-ups and the accepted collapsed-untracked-directory fingerprint limit remain unchanged.
