## ADDED Requirements

### Requirement: Zoom toggle kick dwell and bounded retry backoff

A Ctrl+Z fullscreen toggle that moves a bound Hera pane's width SHALL re-arm the size-drift kick evaluation behind a 100ms dwell (not the 300ms rail-traversal dwell) and SHALL reset the busy-deferral retry backoff to its first interval. The busy-deferral retry interval SHALL start at 10s, double on each consecutive deferral and cap at 15s. A rebind SHALL still reset the backoff. A busy or prompt-blocked agent SHALL still never be killed to repair width.

#### Scenario: Zoom evaluates promptly
- **WHEN** the user toggles fullscreen and the pane's width changes
- **THEN** the kick is evaluated once 100ms have elapsed, not 300ms

#### Scenario: Zoom restarts a long backoff
- **WHEN** a coordinator has been deferring busy until its retry interval reached the cap and the user toggles fullscreen
- **THEN** the next deferral's retry is armed at 10s

#### Scenario: Backoff is capped
- **WHEN** a kick is deferred repeatedly
- **THEN** successive retry delays are 10s, 15s, 15s and so on
