package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/argus-foundry/argus/internal/gitutil"
)

const wtFingerprintLimit = 2000

func statusRevision(msg gitutil.GitStatusRefreshMsg) string { return worktreeRevision("", msg) }

func worktreeRevision(worktree string, msg gitutil.GitStatusRefreshMsg) string {
	b, err := json.Marshal(msg)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	_, _ = hash.Write(b)
	paths := gitutil.ParseGitStatus(msg.Status)
	root, rootErr := os.OpenRoot(worktree)
	if rootErr == nil {
		defer func() { _ = root.Close() }()
	}
	if rootErr == nil && len(paths) <= wtFingerprintLimit {
		for _, file := range paths {
			names := revisionPaths(file.Status, file.Path)
			for _, name := range names {
				if filepath.IsAbs(name) {
					continue
				}
				if fi, err := root.Lstat(name); err == nil {
					_, _ = fmt.Fprintf(hash, "\x00%s\x00%d\x00%d", name, fi.Size(), fi.ModTime().UnixNano())
				}
			}
		}
	}
	return "r" + hex.EncodeToString(hash.Sum(nil)[:8])
}

func revisionPaths(status, path string) []string {
	names := []string{path}
	if strings.ContainsAny(status, "RC") {
		quoted, escaped := false, false
		for i := 0; i < len(path); i++ {
			if escaped {
				escaped = false
				continue
			}
			if quoted && path[i] == '\\' {
				escaped = true
				continue
			}
			if path[i] == '"' {
				quoted = !quoted
				continue
			}
			if !quoted && strings.HasPrefix(path[i:], " -> ") {
				names = []string{path[:i], path[i+4:]}
				break
			}
		}
	}
	for i, name := range names {
		if decoded, err := strconv.Unquote(name); err == nil {
			names[i] = decoded
		}
	}
	return names
}
