# Workspace cost in Info

Show estimated US dollar cost in the selected task's Info tab using the existing Hera roster snapshot. Workers and freelancers show their role cost. Coordinators show their own cost and the total for their coordinator, excluding nested teams. Available REST token totals appear beside the estimate.

No rail or split roster cost, backend changes, extra roster fetch or polling. Existing client convergence follow-ups cover native and TUI placement.

## Abstractions

The Hera roster owns live role bindings, measured cost and token totals. Task cost rows resolve a selected task against that snapshot. TaskTools renders those rows in Info. The existing workspace roster refresh updates TaskTools.
