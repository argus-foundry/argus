## Decisions

- Validation runs before the update map is applied, so a bad style never leaves a partial write of the other sections.
- A stored value that is not a known style (hand-edited DB) is reported as `progress`, matching `spinner.Get`'s fallback the TUI uses.
- Any authenticated token may set it, like `defaults`; it is not a security control.
