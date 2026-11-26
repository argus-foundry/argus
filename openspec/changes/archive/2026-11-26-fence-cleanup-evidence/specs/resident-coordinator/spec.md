## ADDED Requirements

### Requirement: Keep cleanup evidence in its original Git context

Resident stack follow-up SHALL use the captured repository and SHALL skip when current configuration would redirect it. Local and remote cleanup SHALL use one resolved execution directory for admission, identity checks and Git commands. TUI verdict caches SHALL distinguish the repository and configured merge target.

#### Scenario: The captured repository pathname becomes a symlink to another repository
- **WHEN** a later cleanup reopens a path whose canonical Git identity changed
- **THEN** it leaves the new repository alone.

#### Scenario: Project aliases share Git refs but use different default branches
- **WHEN** TUI stacks are evaluated under both default branches
- **THEN** the verdict for one merge target does not authorize cleanup under the other.
