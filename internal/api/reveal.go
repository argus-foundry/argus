package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/argus-foundry/argus/internal/uxlog"
)

type revealReq struct {
	Path string `json:"path"`
}

// revealLauncher runs a host command with an argument vector, never a shell.
type revealLauncher func(name string, args ...string) error

func execRevealLauncher(name string, args ...string) error {
	return exec.Command(name, args...).Run() //nolint:gosec // name is the fixed "open"; args are containment-validated paths
}

func revealInvalidPath(w http.ResponseWriter, taskID, reason string) {
	uxlog.Log("[api] reveal rejected: task=%s reason=%s", taskID, reason)
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_path"})
}

// pathHasGitSegment reports whether any segment of a slash/OS-separated relative
// path is .git (case-insensitive: the macOS default volume is case-insensitive).
func pathHasGitSegment(rel string) bool {
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		if strings.EqualFold(seg, ".git") {
			return true
		}
	}
	return false
}

// handleRevealTask opens a task's worktree, or one contained regular file in it,
// in Finder on the daemon host. Master-token-only: it launches a host process.
func (s *Server) handleRevealTask(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	id := r.PathValue("id")
	var req revealReq
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	dec := json.NewDecoder(r.Body)
	decodeErr := dec.Decode(&req)
	if decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid JSON", decodeErr)
		return
	}
	if decodeErr == nil {
		var trailing any
		if err := dec.Decode(&trailing); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
	}
	task, err := s.db.Get(id)
	if err != nil || task == nil || task.Worktree == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task_not_found"})
		return
	}

	goos := s.revealGOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	launch := s.revealLaunch
	if launch == nil {
		launch = execRevealLauncher
	}

	if req.Path == "" {
		st, err := os.Lstat(task.Worktree)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "path_not_found"})
			return
		}
		if !st.IsDir() {
			revealInvalidPath(w, id, "worktree-not-directory")
			return
		}
	}

	root, err := filepath.EvalSymlinks(task.Worktree)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "path_not_found"})
		return
	}

	target, args := "worktree", []string{"-a", "Finder", root}
	if req.Path != "" {
		rel := req.Path
		if strings.ContainsRune(rel, 0) || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
			revealInvalidPath(w, id, "absolute-or-nul")
			return
		}
		for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
			if seg == ".." {
				revealInvalidPath(w, id, "traversal")
				return
			}
		}
		if pathHasGitSegment(rel) {
			revealInvalidPath(w, id, "git-metadata")
			return
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, rel))
		if err != nil {
			uxlog.Log("[api] reveal missing path: task=%s", id)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "path_not_found"})
			return
		}
		within, err := filepath.Rel(root, resolved)
		if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
			revealInvalidPath(w, id, "escape")
			return
		}
		if pathHasGitSegment(within) {
			revealInvalidPath(w, id, "git-metadata-resolved")
			return
		}
		st, err := os.Stat(resolved)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "path_not_found"})
			return
		}
		if !st.Mode().IsRegular() {
			revealInvalidPath(w, id, "not-regular-file")
			return
		}
		target, args = "file", []string{"-R", resolved}
	}

	if goos != "darwin" {
		uxlog.Log("[api] reveal unsupported host: task=%s goos=%s", id, goos)
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "unsupported_host"})
		return
	}
	if err := launch("open", args...); err != nil {
		uxlog.Log("[api] reveal failed: task=%s target=%s err=%v", id, target, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reveal_failed"})
		return
	}
	uxlog.Log("[api] reveal opened: task=%s target=%s", id, target)
	writeJSON(w, http.StatusOK, map[string]string{"status": "opened", "target": target, "host": "daemon"})
}
