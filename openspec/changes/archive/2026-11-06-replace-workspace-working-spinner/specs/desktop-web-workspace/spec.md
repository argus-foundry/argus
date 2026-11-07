## MODIFIED Requirements

### Requirement: Compact task rows
Task rows SHALL display a single compact line with a status icon, task name and the existing centered hover archive control, retaining nesting and disclosure behavior. Names SHALL truncate with an ellipsis and expose truncated full names through shared data-tooltip help without native title. Rows SHALL omit elapsed time and separate visible status text. Each inline SVG status icon SHALL carry an accessible status label.

#### Scenario: Represent available task states
- **WHEN** a task row renders
- **THEN** active non-idle sessions SHALL show the Scribe, a pen nib writing three short lines, lifting and returning to the start over a 5.6 second loop in the existing fixed 16px icon slot, using currentColor from the rail text token with 1.4px rounded strokes and only transform, opacity and stroke-dashoffset animation, and idle/pending/offline tasks without a PR SHALL show a gray branch icon
- **AND** open/review PRs SHALL show a green pull-request icon, approved PRs a distinguishable green variant, complete tasks a purple merged icon, and needs-input a prominent attention icon
- **AND** failed bound Hera roles SHALL show red failure icons
- **AND** reduced-motion preference SHALL stop all working indicator animation and retain the completed three lines and stationary nib as the static working frame
- **AND** the combined REST `merged-closed` state SHALL use a purple terminal PR icon labeled PR merged or closed without claiming it can distinguish the two
- **AND** task failures outside the Hera roster remain unavailable because the task REST payload has no failure field

#### Scenario: Busy Hera worker already in review
- **WHEN** a session is live and not idle, including a task in review or complete workflow status
- **THEN** the working Scribe SHALL represent its activity regardless of workflow status, retaining archived, needs-input, failure and combined terminal PR precedence
- **AND** an idle task in review without cached PR state SHALL remain gray and labeled In review, since workflow status alone does not establish a PR
