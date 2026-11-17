package gitutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// HandoffState is what the handed-off archive predicate needs to know about a
// task's PR and branch (add-hera-handoff-archive, design D2).
type HandoffState struct {
	PRURL  string
	TipSHA string // the pushed tip == local HEAD == PR head
}

// prHandoffJSON is the subset of `gh pr view --json ...` the handoff check uses.
type prHandoffJSON struct {
	State             string          `json:"state"`
	IsDraft           bool            `json:"isDraft"`
	URL               string          `json:"url"`
	HeadRefOid        string          `json:"headRefOid"`
	StatusCheckRollup []rollupContext `json:"statusCheckRollup"`
}

// rollupContext covers both CheckRun (conclusion/status) and StatusContext (state).
type rollupContext struct {
	Name       string `json:"name"`
	Context    string `json:"context"`
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
	State      string `json:"state"`
}

// failingChecks names the failing entries in a check rollup. Pending / in
// progress / skipped / neutral / absent checks are NOT failing ("not failing",
// not "green"); cancelled, timed-out, startup-failed and action-required are
// treated as failing because the PR cannot be called healthy.
func failingChecks(r []rollupContext) []string {
	var bad []string
	for _, c := range r {
		v := strings.ToUpper(c.Conclusion)
		if v == "" {
			v = strings.ToUpper(c.State) // StatusContext
		}
		switch v {
		case "FAILURE", "ERROR", "TIMED_OUT", "STARTUP_FAILURE", "CANCELLED", "ACTION_REQUIRED":
			n := c.Name
			if n == "" {
				n = c.Context
			}
			bad = append(bad, n)
		}
	}
	return bad
}

// ParsePRHandoff is the pure half of the handoff check: raw `gh pr view` JSON
// to (url, head oid) or a refusal reason. Anything it cannot positively
// confirm is a refusal (fail closed).
func ParsePRHandoff(raw string) (url, headOid, reason string) {
	var p prHandoffJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &p); err != nil {
		return "", "", fmt.Sprintf("unreadable PR lookup (%v)", err)
	}
	if p.State != "OPEN" {
		return "", "", fmt.Sprintf("PR is %s, not open", strings.ToLower(p.State))
	}
	if p.HeadRefOid == "" {
		return "", "", "PR head commit unknown"
	}
	if bad := failingChecks(p.StatusCheckRollup); len(bad) > 0 {
		return "", "", "PR checks failing: " + strings.Join(bad, ", ")
	}
	return p.URL, p.HeadRefOid, ""
}

// handoffGit runs git in dir; a seam so tests need no repository.
var handoffGit = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args[:1], ""), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// CheckHandoff verifies the handed-off predicate's git/PR half for a task's
// worktree and branch: PR open and not failing CI, branch pushed with the
// local tip equal to the remote tip and to the PR head, worktree clean.
// ok=false always carries a reason; every error and timeout is a refusal.
func CheckHandoff(ctx context.Context, worktree, branch string) (st HandoffState, ok bool, reason string) {
	if worktree == "" || branch == "" {
		return st, false, "task has no worktree/branch"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	raw, code, runErr := prRunner(ctx, worktree, "pr", "view", branch, "--json", "state,isDraft,url,headRefOid,statusCheckRollup")
	if runErr != nil || code != 0 {
		if strings.Contains(raw, "no pull requests found") {
			return st, false, "no PR for the branch"
		}
		return st, false, fmt.Sprintf("PR lookup failed (exit %d, %v)", code, runErr)
	}
	url, head, why := ParsePRHandoff(raw)
	if why != "" {
		return st, false, why
	}

	local, err := handoffGit(ctx, worktree, "rev-parse", "HEAD")
	if err != nil || local == "" {
		return st, false, fmt.Sprintf("local tip unreadable (%v)", err)
	}
	lsOut, err := handoffGit(ctx, worktree, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return st, false, fmt.Sprintf("remote tip unreadable (%v)", err)
	}
	remote := ""
	if f := strings.Fields(lsOut); len(f) >= 1 {
		remote = f[0]
	}
	switch {
	case remote == "":
		return st, false, "branch is not pushed to origin"
	case remote != local:
		return st, false, fmt.Sprintf("local tip %s differs from pushed tip %s", short(local), short(remote))
	case head != local:
		return st, false, fmt.Sprintf("PR head %s differs from local tip %s", short(head), short(local))
	}
	dirty, err := handoffGit(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return st, false, fmt.Sprintf("worktree status unreadable (%v)", err)
	}
	if dirty != "" {
		return st, false, "worktree has uncommitted changes"
	}
	return HandoffState{PRURL: url, TipSHA: local}, true, ""
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
