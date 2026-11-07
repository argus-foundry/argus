## Decisions

- **Response read-back.** The handler calls `SetPinned` then `Get` to report the resulting flags; a concurrent writer between the two could change what is reported, never what is stored.
- **`*bool` for presence.** A missing or `null` `pinned` is a 400; a string/number fails decoding and is also a 400. `DisallowUnknownFields` rejects extra keys.
- **No deviation** from the ticket contract.
