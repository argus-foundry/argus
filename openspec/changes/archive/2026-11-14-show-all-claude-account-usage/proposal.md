## Why

The status bar's usage segment shows one Claude reading (the default account) and one Codex reading, e.g. `cla 100%w · cdx 53%`. With a second Claude account configured (Aaron's `personal`), there is no passive way to see whether that account still has headroom — exactly the moment it matters, when the default account is depleted.

## What Changes

- The status bar renders one extra Claude usage entry per configured non-default Claude-capable account, between the default `cla` entry and `cdx`, labelled with the account name, e.g. `cla 100%w · personal 40%s · cdx 53%`.
- Each extra entry uses the same rendering rules as `cla`: fuller still-valid window with its `s`/`w` suffix, `—` when absent or stale.
- The TUI reads these through the per-account usage source it already uses for the account picker (the daemon's existing `Daemon.AccountUsage` RPC), on the same `usageRecheckInterval` poll. No daemon or wire change.
- With no named Claude accounts configured, the segment is unchanged.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `tui-shell`: the status-bar usage readout gains per-account Claude entries.

## Impact

- Code: `internal/tui/widget/statusbar.go`, `internal/tui/app.go` (`reevaluateUsage`).
- No REST/RPC change; works against the already-running daemon.
- Web/macOS have no status-bar usage readout today; parity stays under the existing named follow-up `web-macos-routing-settings-parity`.
