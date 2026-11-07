package gitutil

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Head describes what a worktree's HEAD currently references. Branch is nil
// for a detached HEAD and OID is nil for an unborn branch.
type Head struct {
	Branch   *string `json:"branch"`
	OID      *string `json:"oid"`
	Detached bool    `json:"detached"`
}

// ReadHead resolves the live HEAD of a worktree. A read failure returns an
// error rather than a guessed value.
func ReadHead(worktree string) (Head, error) {
	if worktree == "" {
		return Head{}, errors.New("empty worktree")
	}
	var h Head
	ref, err := runGit(worktree, "symbolic-ref", "-q", "HEAD")
	ref = strings.TrimSpace(ref)
	switch {
	case err == nil && ref != "":
		b := strings.TrimPrefix(ref, "refs/heads/")
		h.Branch = &b
	case err != nil && ref == "" && isExitCode(err, 1):
		h.Detached = true
	default:
		return Head{}, fmt.Errorf("git symbolic-ref HEAD: %w", err)
	}
	if out, err := runGit(worktree, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err == nil {
		if oid := strings.TrimSpace(out); oid != "" {
			h.OID = &oid
		}
	} else if h.Detached || !isExitCode(err, 1) {
		return Head{}, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return h, nil
}

func isExitCode(err error, code int) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == code
}
