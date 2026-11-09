## ADDED Requirements

### Requirement: Server-resolved team details
Coordinator Info SHALL list each team's role archetype and model_resolution, including separate requested_model, effective_model, effort, profile and safe warning. current_config SHALL say resolved from current settings; planned_intent SHALL say planned. Null effective_model with cli_default SHALL say CLI default, unavailable SHALL say unavailable. Existing roster reconciliation and explicit Info refresh SHALL update only the selected team's details. Failed/auth reads SHALL visibly mark retained details stale or unavailable, never fresh.

#### Scenario: Team resolution refresh
- **WHEN** explicit Info refresh obtains changed roster fields
- **THEN** selected team details update without using raw task model as effective model

### Requirement: Measured roster pressure
Worker/freelance rows SHALL display server-provided percent with thresholds 40, 65 and 90 and accessible token/window labels. Coordinator context SHALL say recycle-budget usage. Null context SHALL remain unavailable and null percent SHALL remain uncomputed. Null observed_at SHALL say timing unknown. Archived/dead rows SHALL suppress live-pressure cues. Failed roster reads SHALL mark retained pressure stale. Indicators SHALL require no per-row reads, animation, or color-only explanation.

#### Scenario: Unknown denominator
- **WHEN** context contains tokens with percent null
- **THEN** the row shows tokens and unavailable percentage without inventing a window
