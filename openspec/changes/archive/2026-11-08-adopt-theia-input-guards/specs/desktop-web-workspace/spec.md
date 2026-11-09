## ADDED Requirements

### Requirement: Authoritative input rejection
The workspace SHALL recognize only sanitized coded task_archived 409 input responses, fence primary and split raw/composer input, retain drafts and attachments, reconcile through a fresh task snapshot, and show restore guidance. It SHALL never automatically replay rejected input.

#### Scenario: Archive during stream outage
- **WHEN** input receives task_archived while client state is stale
- **THEN** subsequent input is fenced until a post-rejection task snapshot succeeds and the draft remains available for a deliberate send after restoration

### Requirement: Deliberate closed-out task revival
Resume and restart SHALL offer explicit confirmation only for 409 hera_closeout with can_force_revive true. Confirm SHALL resubmit once with force_revive true. Cancel or any failure SHALL preserve unsent content. Busy and pending-restart conflicts SHALL never force. Sending after resume SHALL await successful resume.

#### Scenario: Revival rejected or canceled
- **WHEN** a closed-out task rejects an ordinary resume
- **THEN** Cancel sends no forced request, and Confirm sends at most one forced request
