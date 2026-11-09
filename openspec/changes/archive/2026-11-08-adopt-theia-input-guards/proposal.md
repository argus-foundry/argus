# Adopt authoritative input and revival guards

Preserve composer drafts and attachments after archived-input rejection. Fence all task input immediately, reconcile task state, and require a deliberate new send after restoration. Resume/restart close-out conflicts offer explicit revival and submit force_revive once only after confirmation. Other conflicts never force. Any resume-and-send sequence must finish resume successfully before sending.

## Non-Goals

No backend changes. macOS follow-up `macos-authoritative-input-and-revival-guards` adopts these guards in the native client. Automatic stopped-task composer resume remains outside this slice; the existing task settings Resume action owns revival.

## Abstraction inventory

| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| Archived input fence | Policy | Block task input until authoritative reconciliation | WorkspaceEvents |
| Task revival | Operation | Confirm only a coded close-out conflict and force once | Api, confirmDialog |
| Composer draft | Entity | Retain text and attachments until accepted | TerminalInput |

Existing owners already separate terminal writes, event reconciliation, and task settings. Shared revival orchestration belongs in a focused task module.
