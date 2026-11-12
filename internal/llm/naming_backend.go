package llm

import "strconv"

// NamingBackend describes an isolated naming invocation. A zero value retains
// the Claude utility for direct callers; task creation always selects explicitly.
type NamingBackend struct {
	Binary     string
	Env        []string
	Args       func(prompt, instructions string) []string
	JSONEvents bool
	Prepare    func(NamingBackend, string) (NamingBackend, func(), error)
}

// CodexNameArgs pins the inexpensive Luna tier, independently of task model
// profiles. Ignore user configuration (including MCP/hooks) but retain login.
func CodexNameArgs(prompt, instructions string) []string {
	return []string{"exec", "--model", "gpt-6-luna", "--ignore-user-config", "--ignore-rules", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never", "--json",
		"-c", "model_reasoning_effort=\"low\"", "-c", "features.shell_tool=false", "-c", "features.apps=false", "-c", "features.browser_use=false", "-c", "features.computer_use=false", "-c", "web_search=\"disabled\"", "-c", "project_doc_max_bytes=0", "-c", "developer_instructions=" + strconv.Quote(instructions), "--", "Task description: " + prompt}
}
