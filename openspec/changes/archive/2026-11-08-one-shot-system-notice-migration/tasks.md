## 1. Implementation

- [x] 1.1 Shared `notSystemSenderSQL` predicate used by cap, UnreadCount and ExcludeSystem
- [x] 1.2 One-shot `MigrateLegacySystemNotices` (marker, mark survivors read, retry on failure) wired in `migrate()`
- [x] 1.3 Tests: migration once-only, retry-after-failure, bridge snapshot with only system notes

## 2. Docs

- [x] 2.1 Update messaging.md gotcha
