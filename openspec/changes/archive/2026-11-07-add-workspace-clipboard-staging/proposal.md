# Workspace clipboard staging

Expose the selected task's staged text beside the terminal with a compact Copy staged text button and a bounded plaintext preview tooltip. Read the existing clipboard endpoint on selection and stream connection; consume clipboard SSE frames without changing terminal output cursors. Use shared clipboard feedback and preserve staged text after copy, matching clipboard-staging semantics.

TaskClipboard owns selected-task staging and its control. TerminalStream delivers clipboard events through AgentTerminal and TaskTerminalCache to that owner. The shared copyNotice helper owns clipboard writes and feedback. Selection and event revisions fence late reads; cleared staging restores focus only when its button owned focus.

No backend, static PWA, TUI or macOS changes. The existing clipboard-staging spec covers TUI semantics; native-workspace-rebuild remains the named macOS convergence follow-up. Cmd+Shift+C retains visible-output copying. No additional shortcut is needed for this standard keyboard-accessible button.
