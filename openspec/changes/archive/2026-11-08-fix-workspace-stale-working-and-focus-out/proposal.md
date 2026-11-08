## Why

The rail kept a coordinator tagged working long after its session went idle, because the tag trusted the self-reported Hera role status, which never resets on its own. Claude Code also never produced a suggested next prompt for the composer while the browser terminal had reported a focus-out, which happens whenever a person leaves the terminal for the composer.

## What Changes

A row's working tag needs a live, non-idle bound task, matching the TUI activity rule. Otherwise it shows ready, idle or stopped. The terminal input path drops the exact focus-out report only for resolved backend roster kinds `claude` and `""` (custom) and keeps focus-in. Missing-task working tags show stopped or ready. Backend classification is prefetched when input attaches. Unknown classifications hold one focus-out for up to two seconds without holding later input, which invalidates a stale report. Failed prefetches retry twice with backoff.

## Non-Goals

No daemon, bridge mod or API change. Claude Code can suppress suggestions for non-allowed rate-limit states, with an explicit allowed_warning override; upstream gating is left alone. The static PWA keeps forwarding focus reports since it has no composer suggestion. macOS follow-up is tracked under `native-workspace-rebuild`.
