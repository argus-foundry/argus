## ADDED Requirements

### Requirement: Keyboard scrolling of context details
The named context and usage popover SHALL be keyboard focusable and receive focus on opening without trapping Tab navigation. Native keyboard scrolling SHALL reach overflowing content at narrow viewport sizes. Refresh SHALL preserve panel focus and scroll position, and Escape SHALL dismiss the popover and return focus to its trigger.

#### Scenario: Overflow at a narrow viewport
- **WHEN** context categories, quotas and account details overflow at 320 by 600 pixels
- **THEN** keyboard opening focuses the named scroll region
- **AND** PageDown scrolls it and End reaches its lower content
- **AND** refreshing category values retains panel focus and scroll
- **AND** Escape closes the popover and focuses the trigger
