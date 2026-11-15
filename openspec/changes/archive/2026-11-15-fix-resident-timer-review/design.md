## Context

The first timer implementation used pending receipts only until notifier submission. Review requires an unread inbox event to remain the coalescing boundary and failure recovery to be independent per timer.

## Goals / non-goals

- A broken timer cannot block healthy fires, queued wake-ups or cancellations.
- Keep one unread event per recurring timer, recover missing messages, and bound terminal-row retention.
- Keep CI batching infrastructure outside the timer PR; preserve the stacked branch history.

## Decisions

- Retain a delivery receipt until acknowledgement, with a durable submitted marker to avoid waking again after a daemon bounce. Coalesce against its unread message, not just notifier registration.
- Process old receipts before firing and again afterward. Missing/read messages cancel old registrations and release receipts; expired notifier registrations can retry without duplicate registration while pending.
- Deactivate an invalid recurrence in its transaction and continue other timers. Unexpected fire errors are reported but cannot skip the delivery pass.
- Record terminal time for one-shots, cancellation and failed recurrence; prune terminal rows older than 30 days only after their receipt is settled, preserving undelivered events during extended downtime.
- Emit message-sent events after commit, using the existing system-message payload shape. Reject `Local` case-insensitively.

## Risks / trade-offs

- Submitted receipts remain until the agent acknowledges its inbox; this intentionally suppresses repeated wake-ups for the same unread loop.
- Terminal history with an unsettled receipt may exceed 30 days so an absent Solo does not lose its wake-up.
- Moving the existing daemon-only state type out of the supervisor manifest changes its bytes once; record the honest digest without a surface bump, as confirmed by the coordinator. Future daemon-only state changes no longer churn that digest.
