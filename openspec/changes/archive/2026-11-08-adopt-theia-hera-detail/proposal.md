# Show resolved Hera models and measured row pressure

Coordinator Info lists its team's archetypes and server-resolved model choice, requested overrides, basis, effort, profile and safe warning. Shared roster reads and explicit Info refresh keep selected team scope; failed reads clearly mark retained details stale. Compact rows consume server-projected context without per-row reads or model resolution in JavaScript.

## Non-Goals
No backend changes or tier editing. Native follow-up `macos-hera-resolution-and-context-presentation` adopts the roster fields in native UI. Selected-task composer context stays separate.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| Team model resolution | Value | Server-resolved current config or planned intent | Hera roster |
| Role context pressure | Value | Observed tokens and configured denominator with timing | Hera roster |
| Coordinator team Info | Visual Component | Present role resolution without runtime claims | Existing Info and roster |

Info currently combines identity and task facts; extract team detail presentation to a focused module. Existing compactTaskRow remains row owner.

Threshold source is `internal/hera/tiering/tiering.go:365`, used by TUI `internal/tui/hera/rail.go:2519`. It defines warm >=40, hot >=65 and critical >=90.
