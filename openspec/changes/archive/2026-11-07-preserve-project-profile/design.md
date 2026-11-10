## Decisions

- `profile` is a `*string` on the request so omitted and `""` differ; responses always set it.
- `PUT` no longer upserts: an unknown project is 404, per the ticket. Fields absent from the REST schema are not stored in the projects table, so nothing else is lost by the single-row replace.
- config.toml ownership: the file loader records which `[projects.<name>]` tables the last overlay defined; any write to such a project is 409, since the file wins wholesale and the write could not take effect.
- "Changed" is judged by re-reading the stored row before and after, so a no-op save emits nothing.
- Profile existence is checked against `~/.argus/profiles` plus the built-in `default`; in-repo profiles are not visible to the daemon-wide API and are not accepted by name.
- `config.changed` is a new event type; another M2 ticket may add the same constant, keep one on merge.
