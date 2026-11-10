package mcp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

// Confinement of agent-supplied paths (add-argus-artifact-publish security
// review). The MCP server runs inside the unsandboxed daemon and agents name
// files for it to read (artifact_publish uploads them, artifact_register
// copies them into storage served over REST). Every such read goes through
// confinedOpen, for every task, sandboxed or not:
//
//   - the caller's task is self-reported (id/cwd; the MCP endpoint is one
//     shared URL with no per-task binding), so confinement is to THAT task's
//     own roots: naming another task only reaches that task's worktree;
//   - open with O_NOFOLLOW (a symlink as the final component is refused) and
//     O_NONBLOCK (a FIFO can't hang the handler), then fstat the descriptor:
//     it must be a regular file with one link (a hard link could alias a file
//     outside the roots);
//   - resolve the path's real location and require it inside the task's
//     writable roots (agent.SandboxWriteRoots) and outside its deny-read paths
//     (agent.SandboxDenyReadPaths);
//   - require the descriptor and the resolved path to be the SAME file
//     (os.SameFile on dev/inode), so a parent directory swapped between the
//     open and the check is caught, and read only from that descriptor.
//
// darwin's F_GETPATH would name the descriptor's path directly, but x/sys
// exposes no wrapper and a raw pointer fcntl is unsafe; the dev/inode match
// gives the same guarantee for a single-linked file, which the link-count
// check enforces.

// SetPathPolicy wires how the server finds a task's writable roots and
// deny-read paths (agent.SandboxWriteRoots / agent.SandboxDenyReadPaths over
// the live config, in the daemon). Unset, both are computed without config,
// so configured extra_write paths are not trusted.
func (s *Server) SetPathPolicy(fn func(task *model.Task) (write, deny []string)) {
	s.pathPolicy = fn
}

func (s *Server) taskPathPolicy(task *model.Task) (write, deny []string) {
	if s.pathPolicy != nil {
		return s.pathPolicy(task)
	}
	return agent.SandboxWriteRoots(task, config.Config{}), agent.SandboxDenyReadPaths(task, config.Config{})
}

// underAny reports whether real lies at or below one of roots (each
// symlink-resolved before comparing).
func underAny(real string, roots []string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if r, err := filepath.EvalSymlinks(root); err == nil {
			root = r
		}
		root = filepath.Clean(root)
		if real == root || strings.HasPrefix(real, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// checkConfinedLocation resolves path and checks it against task's roots and
// deny-read paths, returning the resolved path.
func (s *Server) checkConfinedLocation(task *model.Task, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	write, deny := s.taskPathPolicy(task)
	if !underAny(real, write) {
		return "", fmt.Errorf("%s resolves to %s, outside task %s's writable directories (its worktree, temp dirs and extra_write paths); write the file there first", path, real, task.ID)
	}
	if underAny(real, deny) {
		return "", fmt.Errorf("%s is under a sandbox deny-read path; Argus will not read it", path)
	}
	return real, nil
}

// confinedOpen opens an agent-supplied file for reading on task's behalf,
// enforcing the rules above. The caller reads from (and closes) the returned
// descriptor only.
func (s *Server) confinedOpen(task *model.Task, path string) (*os.File, os.FileInfo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // G304: agent-supplied path, confined below before any byte is read.
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, nil, fmt.Errorf("%s is a symlink; pass the file it points to (inside your worktree or a temp dir)", path)
		}
		return nil, nil, err // *os.PathError already names the path
	}
	ok := false
	defer func() {
		if !ok {
			f.Close() //nolint:errcheck,gosec // failure path
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	if linkCount(info) > 1 {
		return nil, nil, fmt.Errorf("%s has more than one hard link; Argus reads only single-linked files", path)
	}
	real, err := s.checkConfinedLocation(task, abs)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := os.Lstat(real)
	if err != nil || !os.SameFile(info, resolved) {
		return nil, nil, fmt.Errorf("%s changed while it was being checked", path)
	}
	ok = true
	return f, info, nil
}

// confinedRead reads an agent-supplied file of at most maxBytes through
// confinedOpen, from the same descriptor that was checked.
func (s *Server) confinedRead(task *model.Task, path string, maxBytes int64) ([]byte, error) {
	f, info, err := s.confinedOpen(task, path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxBytes)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxBytes)
	}
	return b, nil
}

// linkCount is the file's hard-link count, or 1 when the platform's stat
// carries none.
func linkCount(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink) //nolint:unconvert // Nlink is uint16 on darwin, uint64 on linux
	}
	return 1
}
