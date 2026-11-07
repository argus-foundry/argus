package gitutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func untrackedConfig(worktree, pattern string, names bool) (string, error) {
	args := []string{"config", "--null"}
	if names {
		args = append(args, "--name-only")
	}
	out, err := runGit(worktree, append(args, "--get-regexp", pattern)...)
	if err != nil {
		var status *exec.ExitError
		if !errors.As(err, &status) || status.ExitCode() != 1 || out != "" {
			return "", err
		}
	}
	return out, nil
}

func uncachedTextconvEnv(worktree string) ([]string, error) {
	inherited, err := inheritedGitConfigEnv(worktree)
	if err != nil {
		return nil, err
	}
	out, err := untrackedConfig(worktree, `^diff\..*\.cachetextconv$`, true)
	if err != nil {
		return nil, err
	}
	var entries [][2]string
	for _, key := range strings.Split(out, "\x00") {
		if key != "" {
			entries = append(entries, [2]string{key, "false"})
		}
	}
	return appendGitConfigEnv(inherited, entries)
}

func inheritedGitConfigEnv(worktree string) ([]string, error) {
	if os.Getenv("GIT_CONFIG_PARAMETERS") == "" {
		return nil, nil
	}
	out, err := runGit(worktree, "config", "--null", "--show-scope", "--list")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	var entries [][2]string
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "command" {
			key, value, _ := strings.Cut(fields[i+1], "\n")
			entries = append(entries, [2]string{key, value})
		}
	}
	return appendGitConfigEnv([]string{"GIT_CONFIG_PARAMETERS="}, entries)
}

func capturedTextconvEnv(worktree, rawPath, metadata string, inherited []string) ([]string, error) {
	out, err := untrackedConfig(worktree, `^(diff\..*\.textconv|filter\..*\.(clean|smudge|process|required))$`, false)
	if err != nil {
		return nil, err
	}
	wrapper := filepath.Join(metadata, "textconv.sh")
	var entries [][2]string
	filters := make(map[string]bool)
	for _, entry := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if strings.HasPrefix(key, "filter.") {
			filters[key[:strings.LastIndex(key, ".")]] = true
		} else if value != "" {
			command := quoteGitCommand(wrapper) + " " + quoteGitCommand(value) + " " + quoteGitCommand(rawPath)
			entries = append(entries, [2]string{key, command})
		}
	}
	if len(entries) == 0 {
		return inherited, nil
	}
	const script = `#!/bin/sh
textconv_command=$1
captured_input=$2
exec /bin/sh -c "$textconv_command"' "$@"' "$textconv_command" "$captured_input"
`
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		return nil, err
	}
	// Blob extraction must not run smudge before the snapshot converter.
	for driver := range filters {
		entries = append(entries, [2]string{driver + ".smudge", "cat"}, [2]string{driver + ".process", ""}, [2]string{driver + ".required", "false"})
	}
	return appendGitConfigEnv(inherited, entries)
}

func appendGitConfigEnv(inherited []string, entries [][2]string) ([]string, error) {
	text := os.Getenv("GIT_CONFIG_COUNT")
	for _, entry := range inherited {
		if value, ok := strings.CutPrefix(entry, "GIT_CONFIG_COUNT="); ok {
			text = value
		}
	}
	count := 0
	if text != "" {
		var err error
		count, err = strconv.Atoi(text)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("invalid inherited GIT_CONFIG_COUNT %q", text)
		}
	}
	env := append([]string{}, inherited...)
	for _, entry := range entries {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", count, entry[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count, entry[1]))
		count++
	}
	return append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", count)), nil
}

func quoteGitCommand(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
