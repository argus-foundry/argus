## ADDED Requirements

### Requirement: Plain task recycle seed is marked as Argus-injected

The plain `task_recycle` seed prompt SHALL begin with the `[argus] ` system-turn prefix (`lastexchange.SystemTurnPrefix`), so a session recycled twice with no human message in between does not have the earlier seed quoted back as the user's last message. The resident seed already carries this prefix and is unchanged.

#### Scenario: Plain task seed starts with the system-turn prefix

- **WHEN** a plain task is recycled via task_recycle
- **THEN** its seed prompt starts with `[argus] ` and is skipped by last-exchange extraction on the next recycle
