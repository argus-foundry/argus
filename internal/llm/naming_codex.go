package llm

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PrepareCodexNaming uses an auth-only home: global skills, hooks, MCP servers
// and AGENTS.md must not enter this tiny utility's context.
func PrepareCodexNaming(backend NamingBackend, instructions string) (NamingBackend, func(), error) {
	root, err := os.MkdirTemp("", "argus-name-")
	if err != nil {
		return backend, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	home, err := os.UserHomeDir()
	if err != nil {
		cleanup()
		return backend, nil, err
	}
	source := filepath.Join(home, ".codex")
	env := backend.Env
	if env == nil {
		env = os.Environ()
	}
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if key == "CODEX_HOME" {
			source = value
			continue
		}
		filtered = append(filtered, entry)
	}
	auth := filepath.Join(source, "auth.json")
	if _, err := os.Stat(auth); err == nil {
		if err := os.Symlink(auth, filepath.Join(root, "auth.json")); err != nil {
			cleanup()
			return backend, nil, err
		}
	} else if !os.IsNotExist(err) {
		cleanup()
		return backend, nil, err
	}
	file := filepath.Join(root, "instructions.md")
	if err := os.WriteFile(file, []byte(instructions), 0600); err != nil {
		cleanup()
		return backend, nil, err
	}
	backend.Env = append(filtered, "CODEX_HOME="+root)
	args := backend.Args
	backend.Args = func(prompt, instructions string) []string {
		argv := args(prompt, instructions)
		n := len(argv) - 2
		return append(append(append([]string(nil), argv[:n]...), "-c", "model_instructions_file="+strconv.Quote(file)), argv[n:]...)
	}
	return backend, cleanup, nil
}
