## Decisions

- Authoring guidance is the primary fix; the renderer stays minimal and literal for unknown syntax.
- List prefix = leading spaces, then `- `, `* ` or `N. `. The leading spaces count toward the hanging indent, so nesting is one level per two spaces with no tab handling.
- Non-Goals: the web SPA and macOS app do not render board bodies (no board UI exists in `internal/api/static` or `macos/`; the REST API only serves the raw body), so there is no parity change. A future board UI on those surfaces should match this subset.
