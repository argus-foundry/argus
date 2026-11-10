## Decisions

- Presence is carried through the Go callback (`Update`) instead of re-deriving it from text, which fixes the TUI-side shared store consumers too; the only production subscriber is the stream handler.
- GET already returned 200 `{"text":""}` for a present empty value, so no handler change there.
- The stream already carries text and presence in one channel struct; the bug was only the derivation.
