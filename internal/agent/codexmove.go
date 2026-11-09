package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/argus-foundry/argus/internal/codexctx"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/skills"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// codexMoveLoginCheck probes the target account's sign-in; tests replace it.
var codexMoveLoginCheck = CodexLoginStatus

// codexThreadColumns are the thread-index columns a move copies. Identity
// columns (creator_*), folder/section links and anything else are left to the
// target's defaults so no cross-account identity or foreign key travels along.
var codexThreadColumns = []string{
	"id", "rollout_path", "created_at", "updated_at", "source", "model_provider",
	"cwd", "title", "sandbox_policy", "approval_mode", "tokens_used",
	"has_user_event", "archived", "archived_at", "git_sha", "git_branch",
	"git_origin_url", "cli_version", "first_user_message", "model",
	"reasoning_effort", "preview", "name", "created_at_ms", "updated_at_ms",
	"history_mode",
}

// codexTransferSession copies the task's Codex thread (its rollout file and
// its row in state_5.sqlite) from the source account's home into the target's.
// Only validated conversation files are copied. Nothing on the source is
// modified or deleted, and the target's own sign-in is verified first.
func codexTransferSession(from, to *model.Task, cfg config.Config) (bool, error) {
	fromName, toName := NormalizeAccount(from.Account), NormalizeAccount(to.Account)
	srcHome, err := CodexHomeForTask(from, cfg)
	if err != nil {
		return false, fmt.Errorf("%w: source account %q: %w", ErrAccount, fromName, err)
	}
	dstHome, err := CodexHomeForTask(to, cfg)
	if err != nil {
		return false, fmt.Errorf("%w: target account %q: %w", ErrAccount, toName, err)
	}
	srcDB, err := CodexStateDBPath(srcHome)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrAccount, err)
	}
	dstDB, err := CodexStateDBPath(dstHome)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrAccount, err)
	}

	srcStorage, dstStorage := srcHome, dstHome
	if srcStorage == "" {
		srcStorage, err = skills.UserCodexHome()
		if err != nil {
			return false, err
		}
	}
	if dstStorage == "" {
		dstStorage, err = skills.UserCodexHome()
		if err != nil {
			return false, err
		}
	}

	if fromName != toName && sameCodexMoveHome(srcStorage, dstStorage, srcDB, dstDB) {
		return false, fmt.Errorf("%w: accounts %q and %q resolve to the same Codex home", ErrAccount, fromName, toName)
	}

	if dstHome != "" {
		if err := BootstrapCodexHome(dstHome); err != nil {
			return false, fmt.Errorf("%w: %w", ErrAccount, err)
		}
	}
	loggedIn, lerr := codexMoveLoginCheck(context.Background(), dstHome)
	switch {
	case lerr != nil:
		uxlog.Log("[account] move: codex sign-in of %q not verified: %v", toName, lerr)
	case !loggedIn:
		return false, fmt.Errorf("%w: account %q is not signed in to Codex", ErrAccount, toName)
	}

	if err := validateCodexMoveTarget(dstDB); err != nil {
		uxlog.Log("[account] move: codex target %s not ready: %v", toName, err)
		return false, fmt.Errorf("%w: account %q has no usable Codex state database; run Codex once under it first: %w", ErrAccount, toName, err)
	}
	sid := from.SessionID
	if sid == "" {
		sid, err = captureCodex(from, cfg)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("%w: cannot identify Codex conversation before moving: %w", ErrAccount, err)
		}
		uxlog.Log("[account] move: discovered codex thread task=%s sid=%s", from.ID, sid)
	}
	if !codexSessionIDRe.MatchString(sid) {
		return false, fmt.Errorf("%w: invalid Codex session ID", ErrAccount)
	}
	if fromName == toName {
		resumed := codexThreadResumable(srcDB, sid)
		if resumed {
			to.SessionID = sid
		}
		return resumed, nil
	}
	row, err := readCodexThread(srcDB, sid)
	if err != nil {
		return false, fmt.Errorf("read codex thread: %w", err)
	}
	if row == nil {
		uxlog.Log("[account] move: codex thread %s not in %s; nothing to carry over", sid, srcDB)
		return false, nil
	}

	srcRollout, _ := row["rollout_path"].(string)
	cwd, _ := row["cwd"].(string)
	err = transferCodexRollout(srcStorage, dstStorage, cwd, codexctx.Thread{ID: sid, RolloutPath: srcRollout}, func(dst string) error {
		row["rollout_path"] = dst
		return upsertCodexThread(dstDB, row)
	})
	if errors.Is(err, errCodexRolloutMissing) {
		uxlog.Log("[account] move: codex rollout for %s missing; nothing to carry over", sid)
		return false, nil
	}
	if err != nil {
		uxlog.Log("[account] move: codex transfer task=%s failed: %v", from.ID, err)
		return false, fmt.Errorf("transfer codex thread to account %q: %w", toName, err)
	}
	to.SessionID = sid
	uxlog.Log("[account] move: codex thread %s copied %s→%s", sid, fromName, toName)
	return true, nil
}

// codexThreadResumable reports whether the task's thread and rollout exist in
// the index at dbPath.
func codexThreadResumable(dbPath, sid string) bool {
	if sid == "" || !codexSessionIDRe.MatchString(sid) {
		return false
	}
	row, err := readCodexThread(dbPath, sid)
	if err != nil || row == nil {
		return false
	}
	rollout, _ := row["rollout_path"].(string)
	info, err := os.Stat(rollout)
	return rollout != "" && err == nil && info.Mode().IsRegular()
}
