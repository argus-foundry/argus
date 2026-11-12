## MODIFIED Requirements

### Requirement: Worktree revision cache

The daemon SHALL maintain, per task worktree, a cached git status and an opaque revision derived from the status message, its comparison, and path/size/mtime-nanosecond metadata for at most 2000 dirty status entries. Above that cap it SHALL use the summary-only hash; edits preserving both size and mtime are outside the metadata fingerprint guarantee. Filesystem changes in the worktree and HEAD/index changes in its git directory SHALL mark the cache dirty, and bursts SHALL coalesce into a single-flight refresh. The cache SHALL be keyed on the task's comparison base and SHALL be discarded when the task is deleted or archived or its worktree disappears. Registration and archive removal SHALL share a lock, and registration SHALL check the current archive state under that lock. Watch roots SHALL resolve symlinks before traversal. Watch budgets SHALL be cumulative per entry and process-wide. Darwin accounting SHALL include directory children opened implicitly by fsnotify; Linux SHALL count directory watches. Failed allocation or excess budget SHALL close the entire entry watcher and degrade it to max-age refresh. Darwin growth checks enforce an approximate budget by recount and degradation, rather than preventing each internal kqueue open. Reservations SHALL be reconciled immediately after Add and during cached refreshes so files introduced during registration cannot remain uncharged. Teardown SHALL explicitly remove attempted watches before closing fsnotify, including partial Add failures. Reservations SHALL NOT become reusable if removal or closure cannot be confirmed.

#### Scenario: Edit changes the revision
- **WHEN** a watched dirty file changes size or mtime within the fingerprint cap, or its status summary changes
- **THEN** a `worktree.changed` event with a new revision is published

#### Scenario: Burst coalesces
- **WHEN** many writes land within the debounce window
- **THEN** at most one refresh runs per window and an unchanged resulting revision publishes nothing
#### Scenario: Overlapping writes
- **WHEN** another state arrives during an in-flight refresh
- **THEN** one follow-up refresh captures it, with at most one concurrent fetch per entry

#### Scenario: Archived read
- **WHEN** an archived task is inspected after watcher cleanup
- **THEN** no persistent watcher or cache is recreated

#### Scenario: Files appear during registration
- **WHEN** files are created after the reservation walk but before fsnotify Add establishes its baseline
- **THEN** the post-Add recount charges them or degrades and tears down the whole entry

#### Scenario: Repeated teardown does not exhaust descriptors
- **WHEN** normal and degraded watchers are repeatedly registered and removed
- **THEN** actual open descriptors return to their baseline, and failed teardown retains its reservation instead of advertising free capacity
