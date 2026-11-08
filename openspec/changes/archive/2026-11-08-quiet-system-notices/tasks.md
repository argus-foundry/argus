## 1. DB layer

- [x] 1.1 Tests then code: coalescing `InsertSystemMessage`, no cap for system notes, `UnreadCount`/cap exclude system sender
- [x] 1.2 Tests then code: idempotent startup collapse of system-note backlog in `migrate`

## 2. Surfaces

- [x] 2.1 Bridge UI snapshot excludes system notes (test)
- [x] 2.2 REST inbox `unread_count` excludes system notes (test)
- [x] 2.3 TUI inbox rows for system notes not unread (test)

## 3. Docs

- [x] 3.1 Gotchas in messaging.md and daemon-rpc.md, knowledge index
- [x] 3.2 Archive change, `make pre-pr`, review, PR
