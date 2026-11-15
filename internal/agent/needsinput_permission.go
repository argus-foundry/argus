package agent

import "strings"

// PermissionPrompt identifies the native permission chooser, using rendered
// cells so cursor-addressed PTY updates do not join adjacent words together.
// Questions and plan dialogs remain outside the resident hook debounce.
func (v *TailView) PermissionPrompt() bool {
	return v.PermissionPromptKey() != ""
}

// PermissionPromptKey identifies the visible chooser rather than general
// question prose. It is deliberately independent of hook traffic.
func (v *TailView) PermissionPromptKey() string {
	text := v.windowText()
	if v.renderer != nil {
		text = v.rendered()
	}
	for _, marker := range []string{"Bash command", "Permission rule", "requires permission"} {
		if start := strings.LastIndex(text, marker); start >= 0 {
			return strings.TrimSpace(text[start:])
		}
	}
	return ""
}
