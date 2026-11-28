## Why

A local daemon can hold its task mutation lock until after checked worker deletion returns. Final removal may need to wait for the exit callback, so the authorized branch lease must survive that wait. A browser task action can also return after the user opens a different detail view.

## What Changes

- Distinguish a stopped process from retirement of its owner slot.
- Carry the original remote branch lease through deferred resident cleanup and report whether removal is pending.
- Scope PWA detail mutation results to their initiating visit.

## Impact

Resident worker cleanup shares session and Git state with the daemon. The detail action fence applies to the PWA.
