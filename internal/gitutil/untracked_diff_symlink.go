package gitutil

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func renderCapturedFile(worktree, private, gitDir, filePath string, symlink bool) (string, error) {
	metadata, err := os.MkdirTemp("", "argus-captured-diff-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(metadata) }()
	objects := filepath.Join(metadata, "objects")
	if err := os.Mkdir(objects, 0o700); err != nil {
		return "", err
	}
	env := []string{
		"GIT_OBJECT_DIRECTORY=" + objects,
		"GIT_INDEX_FILE=" + filepath.Join(metadata, "index"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=",
	}
	base := []string{"--git-dir=" + gitDir, "--work-tree=" + private,
		"-c", "core.splitIndex=false", "-c", "core.fsmonitor=false", "-c", "core.sparseCheckout=false"}
	cacheEnv, err := uncachedTextconvEnv(worktree)
	if err != nil {
		return "", err
	}
	env = append(env, cacheEnv...)
	file, err := os.Open(filepath.Join(private, filePath))
	if err != nil {
		return "", err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return "", err
	}
	mode := "100644"
	if symlink {
		mode = "120000"
	} else if info.Mode()&0o111 != 0 {
		mode = "100755"
	}
	canonical := filepath.Clean(filePath)
	conversion := "--path=" + canonical
	if symlink {
		conversion = "--no-filters"
	}
	blob, err := runGitWithInput(worktree, file, env, append(base, "-c", "core.safecrlf=warn", "hash-object", "-w", conversion, "--stdin")...)
	file.Close()
	if err != nil {
		return "", err
	}
	empty, err := runGitWithInput(private, nil, env, append(base, "hash-object", "-w", "-t", "tree", "--stdin")...)
	if err != nil {
		return "", err
	}
	cacheInfo := mode + "," + strings.TrimSpace(blob) + "," + canonical
	if _, err := runGitWithInput(private, nil, env, append(base, "update-index", "--add", "--cacheinfo", cacheInfo)...); err != nil {
		return "", err
	}
	if symlink {
		if err := os.Remove(filepath.Join(private, filePath)); err != nil {
			return "", err
		}
	}
	originalObjects, err := runGit(worktree, "rev-parse", "--git-path", "objects")
	if err != nil {
		return "", err
	}
	originalObjects = strings.TrimSpace(originalObjects)
	if !filepath.IsAbs(originalObjects) {
		originalObjects = filepath.Join(worktree, originalObjects)
	}
	originalObjects, err = filepath.Abs(originalObjects)
	if err != nil {
		return "", err
	}
	alternates := strconv.Quote(originalObjects)
	if inherited := os.Getenv("GIT_ALTERNATE_OBJECT_DIRECTORIES"); inherited != "" {
		alternates += string(os.PathListSeparator) + inherited
	}
	env = append(env, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+alternates)
	prefixes, err := noIndexPrefixes(worktree, filePath)
	if err != nil {
		return "", err
	}
	if !symlink {
		renderEnv, err := capturedTextconvEnv(worktree, filepath.Join(private, filePath), metadata, cacheEnv)
		if err != nil {
			return "", err
		}
		env = append(env, renderEnv...)
	}
	args := append(base, "--literal-pathspecs", "diff", "--cached", strings.TrimSpace(empty))
	args = append(args, prefixes...)
	args = append(args, "--", canonical)
	out, err := runGitWithInput(worktree, nil, env, args...)
	if err != nil {
		return "", err
	}
	return out, nil
}

func noIndexPrefixes(worktree, filePath string) ([]string, error) {
	out, err := runGit(worktree, "config", "--null", "--get-regexp", `^diff\.(noprefix|mnemonicprefix|srcprefix|dstprefix)$`)
	if err != nil {
		var status *exec.ExitError
		if !errors.As(err, &status) || status.ExitCode() != 1 || out != "" {
			return nil, err
		}
	}
	config := make(map[string]string)
	for _, entry := range strings.Split(out, "\x00") {
		if key, value, ok := strings.Cut(entry, "\n"); ok {
			config[strings.ToLower(key)] = value
		}
	}
	source, ok := config["diff.srcprefix"]
	if !ok {
		source = "a/"
	}
	destination, ok := config["diff.dstprefix"]
	if !ok {
		destination = "b/"
	}
	if gitConfigBool(config, "diff.noprefix") {
		source, destination = "", ""
	} else if gitConfigBool(config, "diff.mnemonicprefix") {
		source, destination = "1/", "2/"
	}
	args := []string{}
	if filePath != filepath.Clean(filePath) {
		displayDir, _ := filepath.Split(filePath)
		relative := filepath.Dir(filepath.Clean(filePath))
		if relative == "." {
			relative = ""
		}
		args = append(args, "--relative="+relative)
		source += displayDir
		destination += displayDir
	}
	return append(args, "--src-prefix="+source, "--dst-prefix="+destination), nil
}

func gitConfigBool(config map[string]string, key string) bool {
	value, present := config[key]
	if !present {
		return false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "true" || value == "yes" || value == "on" {
		return true
	}
	integer, err := strconv.ParseInt(value, 10, 64)
	return err == nil && integer != 0
}
