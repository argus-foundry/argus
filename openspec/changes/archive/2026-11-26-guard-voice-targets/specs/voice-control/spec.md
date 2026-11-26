## ADDED Requirements

### Requirement: Voice lookup and input preserve existing session safeguards

A missing explicit task ID SHALL return the existing missing-workflow result. Voice send SHALL reject the Ctrl+Z control character before starting a job.

#### Scenario: A stored task ID no longer exists

- **WHEN** a voice command addresses a task that was removed
- **THEN** the command reports `not_found` and accepts no job

#### Scenario: Input could suspend the attached session

- **WHEN** a voice send contains the Ctrl+Z control character
- **THEN** the command refuses the input without accepting a job or writing to the session
