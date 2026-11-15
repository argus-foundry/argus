## Context

Resident timers persist inbox fires and coalesce missed occurrences. Memory lives at `resident.memory_dir`, independently of the resident's display name.

## Goals / Non-goals

Goals: one restart-safe config-owned nightly timer, protected from Solo cancellation, with actionable instructions and a short memory change log.

Non-goals: automatic filesystem edits by the daemon, shared KB curation, new timer controls in TUI/web/macOS. All three surfaces can access the same REST timer state and inbox; ownership is exposed through the existing REST and timer tool results. This setting is config-file only for every frontend; no new client-specific control or gap is introduced.

## Decisions

- Use a unique built-in identity mapping to an ordinary durable resident timer. Reconciliation updates that timer in a transaction and preserves its due time when config is unchanged, including overdue fires. A separate identity table avoids deriving ownership from mutable note text.
- Default to 02:30 in the daemon's local timezone; accept strict HH:MM or off. Reconcile before firing on every timer poll so config changes take effect without extra wake-up plumbing.
- Exclude built-ins from Solo's active timer limit. Reject cancellation in the store so every agent path is protected. Disable/config changes retract pending fires through the existing cancellation receipts.
- Deliver the procedure as orientation text embedded in every fire, with the configured memory directory. Use KB tools and preserve dream: skip frontmatter.

## Risks / Trade-offs

- DST can skip a nonexistent local time → reuse existing cron semantics and coalescing; do not invent catch-up tasks.
- Agent curation is judgment-based → preserve explicit instructions/preferences, require dated evidence for conflicting facts and retain uncertain loops for attention.
- A submitted wake-up cannot be recalled → config retracts unsubmitted messages using existing delivery semantics.
