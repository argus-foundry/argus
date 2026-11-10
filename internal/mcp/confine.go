package mcp

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

// Confinement of agent-supplied paths (add-argus-artifact-publish security
// reviews). The MCP server runs inside the unsandboxed daemon and agents name
// files for it to read (artifact_publish uploads them, artifact_register
// copies them into storage served over REST). Every such read goes through
// confinedOpen, for every task, sandboxed or not:
//
//   - the caller's task is self-reported (id/cwd; the MCP endpoint is one
//     shared URL with no per-task binding), so confinement is to THAT task's
//     own roots: naming another task only reaches that task's worktree;
//   - open with O_NOFOLLOW (a symlink as the final component is refused) and
//     O_NONBLOCK (a FIFO can't hang the handler), then fstat the descriptor:
//     a regular file with one link (a hard link could alias a file outside
//     the roots);
//   - take the file's location from the OPEN DESCRIPTOR (darwin F_GETPATH, in
//     the filesystem's canonical case) and require it inside the task's
//     writable roots (agent.SandboxWriteRoots) and outside its deny-read paths
//     (agent.SandboxDenyReadPaths), both canonicalized the same way, so a
//     case variant (~/.AWS) or a symlinked parent can't slip past;
//   - require the descriptor and that path to be the same file, re-fstat the
//     descriptor (still regular, still one link), and read only from it.
//
// Off darwin there is no F_GETPATH: the location comes from resolving the
// name (no case canonicalization) and the same-file check is what catches a
// parent swapped after the open. See descriptorPathCanonical.
//
// Every refusal returns the same errNotAllowedSource, so a caller can't use
// the tools as an existence or permission oracle for files outside its
// roots; the specific reason is logged daemon-side under [artifacts].

// errNotAllowedSource is the one error every confinement refusal returns.
var errNotAllowedSource = errors.New("path is not an allowed artifact source (use a regular file in your task's worktree or a temp dir)")

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

// canonicalPath returns p as the filesystem names it: symlinks resolved and,
// on darwin, the on-disk case (from the opened directory's descriptor). A
// path that can't be opened is returned symlink-resolved, or as given.
func canonicalPath(p string) string {
	if p == "" {
		return p
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if f, err := os.Open(p); err == nil { //nolint:gosec // G304: a configured root/deny path, opened only to read its canonical name.
		defer f.Close() //nolint:errcheck // read-only
		if dp, err := descriptorPath(f); err == nil {
			return filepath.Clean(dp)
		}
	}
	return filepath.Clean(p)
}

// underAny reports whether real lies at or below one of roots, comparing
// canonical forms of each root.
func underAny(real string, roots []string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = canonicalPath(root)
		if real == root || strings.HasPrefix(real, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// refuseSource logs why an agent-supplied path was refused and returns the
// one generic error callers see.
func refuseSource(task *model.Task, path, reason string, err error) error {
	if err != nil {
		reason += ": " + err.Error()
	}
	log.Printf("[artifacts] refused source: task=%s path=%q reason=%s", task.ID, path, reason)
	return errNotAllowedSource
}

// checkLocation checks a descriptor-derived path against task's roots and
// deny-read paths.
func (s *Server) checkLocation(task *model.Task, path, real string) error {
	write, deny := s.taskPathPolicy(task)
	if !underAny(real, write) {
		return refuseSource(task, path, "outside the task's writable directories ("+real+")", nil)
	}
	if underAny(real, deny) {
		return refuseSource(task, path, "under a sandbox deny-read path ("+real+")", nil)
	}
	return nil
}

// checkRegularSingle requires a regular file with exactly one hard link.
func checkRegularSingle(task *model.Task, path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return refuseSource(task, path, "not a regular file", nil)
	}
	if linkCount(info) != 1 {
		return refuseSource(task, path, fmt.Sprintf("%d hard links", linkCount(info)), nil)
	}
	return nil
}

// confinedOpen opens an agent-supplied file for reading on task's behalf,
// enforcing the rules above. The caller reads from (and closes) the returned
// descriptor only.
func (s *Server) confinedOpen(task *model.Task, path string) (*os.File, os.FileInfo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, refuseSource(task, path, "resolve", err)
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // G304: agent-supplied path, confined below before any byte is read.
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, nil, refuseSource(task, path, "final component is a symlink", nil)
		}
		return nil, nil, refuseSource(task, path, "open", err)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close() //nolint:errcheck,gosec // failure path
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, refuseSource(task, path, "fstat", err)
	}
	if err := checkRegularSingle(task, path, info); err != nil {
		return nil, nil, err
	}
	real, err := descriptorPath(f)
	if err != nil {
		return nil, nil, refuseSource(task, path, "descriptor path", err)
	}
	real = filepath.Clean(real)
	if err := s.checkLocation(task, path, real); err != nil {
		return nil, nil, err
	}
	if resolved, err := os.Lstat(real); err != nil || !os.SameFile(info, resolved) {
		return nil, nil, refuseSource(task, path, "changed while it was being checked", err)
	}
	// Re-check the same descriptor after the location check: a link added
	// meanwhile must not slip through.
	info, err = f.Stat()
	if err != nil {
		return nil, nil, refuseSource(task, path, "fstat", err)
	}
	if err := checkRegularSingle(task, path, info); err != nil {
		return nil, nil, err
	}
	ok = true
	return f, info, nil
}

// confinedDir checks an agent-supplied directory (artifact_register's folder
// form) the same way, from an opened descriptor. Each member is then opened
// through confinedOpen.
func (s *Server) confinedDir(task *model.Task, path string) error {
	f, err := os.Open(path) //nolint:gosec // G304: agent-supplied dir, confined below before it is walked.
	if err != nil {
		return refuseSource(task, path, "open", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return refuseSource(task, path, "not a directory", err)
	}
	real, err := descriptorPath(f)
	if err != nil {
		return refuseSource(task, path, "descriptor path", err)
	}
	return s.checkLocation(task, path, filepath.Clean(real))
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
