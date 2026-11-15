package agent

import "strings"

// PermissionPrompt identifies the native permission chooser, using rendered
// cells so cursor-addressed PTY updates do not join adjacent words together.
// Questions and plan dialogs remain outside the resident hook debounce.
func (v *TailView) PermissionPrompt() bool {
	text := v.windowText()
	if v.renderer != nil {
		text = v.rendered()
	}
	return strings.Contains(text, "Bash command") || strings.Contains(text, "Permission rule") || strings.Contains(text, "requires permission") || strings.Contains(text, "Do you want to proceed?")
}
