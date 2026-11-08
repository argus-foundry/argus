## Decisions

- Message pages use `id` as cursor (`id < before_id`, newest page selected by id desc, returned oldest first), diverging from `HeraMessagesForTask`'s `sent_at` ordering because timestamps collide.
- Board mutations use the stored `updated_at` string as the revision token; comparison is exact RFC3339Nano.
- Events are emitted in the `db` mutators after the lock is released so all callers share them.

## Deviations

- Role names fall back to `(deleted role)`, but `hera_messages` cascades with roles, so the fallback is defensive and untestable through the API.
- A non-coordinator role on a mutation returns 400 `invalid_request` (matching the existing GET) rather than 404.
- Dismissing an already-resolved item with a different note returns 409 `item_resolved`.
