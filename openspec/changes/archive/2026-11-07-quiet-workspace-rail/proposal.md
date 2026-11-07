# Quiet workspace rail

## Why
Current task trees need no triage heading. Deferred work should stay reachable without competing with work in progress.

## What changes
Render In progress trees and ordinary tasks directly below their project. Keep Blocked trees expanded with a secondary label. Render nonempty Backlog and Done footers, collapsed by default with guarded browser preferences. Preserve attention, search context, navigation, pinning, keyboard focus and row geometry. Use plain task and team copy throughout workspace controls.

## Scope
Frontend only. Existing REST values and targets remain unchanged. TUI counted groups and focus folding remain intentional presentation differences. macOS presentation parity remains in native-workspace-rebuild and add-web-hera-mutations.

Initial task selection and explicit navigation reveal their destination tree. Other deferred footers retain their saved disclosure or collapsed default.
