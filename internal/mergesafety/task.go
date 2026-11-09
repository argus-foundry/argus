package mergesafety

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
)

// CandidateForTask is the common repo/default-branch resolution for cleanup
// batches and a resident worker's single-task classification.
func CandidateForTask(ctx context.Context, cfg config.Config, t *model.Task) Candidate {
	c := Candidate{ID: t.ID, Branch: t.Branch, TaskCreatedAt: t.CreatedAt}
	if t.IsResident() {
		return c
	}
	p := cfg.Projects[t.Project]
	if p.Path == "" {
		return c
	}
	c.RepoDir = p.Path
	if slug, ok := gitutil.ResolveDefaultRepo(ctx, p.Path); ok {
		c.RepoSlug = slug
	}
	if short, ref, err := gitutil.ResolveDefaultBranch(p.Path, p.Branch); err == nil {
		c.DefaultShort = short
		c.DefaultRef = ref
	}
	return c
}

func ClassifyTask(ctx context.Context, cfg config.Config, t *model.Task) (Verdict, error) {
	if t.IsResident() {
		return Verdict{Reason: "resident is protected"}, nil
	}
	c := CandidateForTask(ctx, cfg, t)
	return Classify(ctx, Params{RepoDir: c.RepoDir, RepoSlug: c.RepoSlug, Branch: c.Branch, DefaultRef: c.DefaultRef, DefaultShort: c.DefaultShort, TaskCreatedAt: c.TaskCreatedAt})
}

// WorkSummary is captured before reclaim. Every unavailable field carries a
// reason, so force deletion never silently loses the accounting of lost work.
type WorkSummary struct {
	Branch       string `json:"branch"`
	CommitsAhead string `json:"commits_ahead"`
	Subjects     string `json:"subjects"`
	DirtyCount   string `json:"dirty_count"`
	PRState      string `json:"pr_state"`
}

func unknown(err error) string { return fmt.Sprintf("unknown (%v)", err) }

func taskGit(ctx context.Context, t *model.Task, args ...string) (string, error) {
	if t.Worktree == "" {
		return "", fmt.Errorf("no worktree")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.Worktree
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func SummarizeTask(ctx context.Context, cfg config.Config, t *model.Task, prState string) WorkSummary {
	s := WorkSummary{Branch: t.Branch, PRState: prState}
	if s.Branch == "" {
		s.Branch = "unknown (no branch recorded)"
	}
	if s.PRState == "" {
		s.PRState = "unknown (no cached PR state)"
	}
	base := t.BaseBranch
	if base == "" {
		p := cfg.Projects[t.Project]
		_, ref, err := gitutil.ResolveDefaultBranch(p.Path, p.Branch)
		if err == nil {
			base = ref
		}
	}
	if base == "" {
		s.CommitsAhead = "unknown (no base ref)"
		s.Subjects = s.CommitsAhead
	} else {
		// Resolve before constructing a range: a stored ref must not become an option.
		baseSHA, err := taskGit(ctx, t, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
		branchSHA := ""
		if err == nil {
			branchSHA, err = taskGit(ctx, t, "rev-parse", "--verify", "--end-of-options", t.Branch+"^{commit}")
		}
		if err != nil {
			s.CommitsAhead = unknown(err)
			s.Subjects = s.CommitsAhead
		} else {
			s.CommitsAhead, err = taskGit(ctx, t, "rev-list", "--count", baseSHA+".."+branchSHA)
			if err != nil {
				s.CommitsAhead = unknown(err)
			}
			s.Subjects, err = taskGit(ctx, t, "log", "-10", "--format=%s", baseSHA+".."+branchSHA)
			if err != nil {
				s.Subjects = unknown(err)
			}
		}
	}
	// -z counts filenames correctly even when a name contains a newline. Rename
	// entries include a second path and still count as one changed file.
	if t.Worktree == "" {
		s.DirtyCount = "unknown (no worktree)"
		return s
	}
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	cmd.Dir = t.Worktree
	out, err := cmd.Output()
	if err != nil {
		s.DirtyCount = unknown(err)
	} else {
		n := 0
		entries := strings.Split(string(out), "\x00")
		for i := 0; i < len(entries); i++ {
			e := entries[i]
			if len(e) < 3 {
				continue
			}
			n++
			if e[0] == 'R' || e[0] == 'C' || e[1] == 'R' || e[1] == 'C' {
				i++
			}
		}
		s.DirtyCount = strconv.Itoa(n)
	}
	return s
}

// Empty fails closed on every missing/unreadable git field.
func (s WorkSummary) Empty() bool { return s.CommitsAhead == "0" && s.DirtyCount == "0" }
