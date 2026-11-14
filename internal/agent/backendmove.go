package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/lastexchange"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	backendMoveMetaNS         = "backend_move"
	backendMoveMetaOrigPrompt = "original_prompt"
	handoffGitTimeout         = 5 * time.Second
)

// backendMoveRunner is the in-place restart with a fresh context a backend
// move needs on top of accountMoveRunner.
type backendMoveRunner interface {
	Recycle(task *model.Task, cfg config.Config, rows, cols uint16) error
}

// BackendLabel names a backend family for humans ("Claude", "Codex").
func BackendLabel(k backendkind.Kind) string {
	if k == backendkind.Codex {
		return "Codex"
	}
	return "Claude"
}

// MoveTaskBackend moves a task between the Claude and Codex CLIs by a restart
// in place SEEDED WITH A HANDOFF. The conversation cannot carry over (the
// transcript formats differ), so the result always reports
// ConversationCarried=false. The task row, worktree, branch and hera bindings
// are kept; backend, account, model, session id and prompt are rewritten in one
// targeted write and all restored when the restart of a live session fails.
func MoveTaskBackend(database *db.DB, runner accountMoveRunner, in AccountMoveInput) (*model.Task, AccountMoveResult, error) {
	if !claimAccountMove(in.TaskID) {
		return nil, AccountMoveResult{}, errAccountMovePending
	}
	defer releaseAccountMove(in.TaskID)
	if err := refuseMoveOfPendingGraduate(database, in.TaskID); err != nil {
		return nil, AccountMoveResult{}, err
	}
	task, err := database.Get(in.TaskID)
	if err != nil {
		return nil, AccountMoveResult{}, err
	}
	if task.IsResident() {
		// The resident's permission policy is Claude-only (--settings rules,
		// forced permission mode); it never moves to another CLI.
		return nil, AccountMoveResult{}, fmt.Errorf("%w: %w", ErrAccount, db.ErrResidentProtected)
	}
	cfg := database.Config()
	cur, err := ResolveBackend(task, cfg)
	if err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: %w", ErrAccount, err)
	}
	curKind := backendkind.Of(cur.Command)
	toName := strings.TrimSpace(in.Backend)
	to, ok := cfg.Backends[toName]
	if !ok {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: backend %q not found in config", ErrAccount, toName)
	}
	toKind := backendkind.Of(to.Command)
	if (curKind != backendkind.Claude && curKind != backendkind.Codex) || (toKind != backendkind.Claude && toKind != backendkind.Codex) {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: a backend move only supports Claude and Codex (from %q to %q)", ErrAccount, task.Backend, toName)
	}
	if curKind == toKind {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: backend %q is the same CLI as the task's; use a plain account move", ErrAccount, toName)
	}
	account := strings.TrimSpace(in.Account)
	if account == "" {
		account = config.DefaultAccountName
	}
	if account == AutoMoveTarget {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: auto is not supported for a backend move; name an account or default", ErrAccount)
	}
	if err := cfg.ValidateAccount(account); err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: %w", ErrAccount, err)
	}
	if !cfg.AccountSupports(account, to.Command) {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: account %q does not support backend %q", ErrAccount, account, toName)
	}
	account = NormalizeAccount(account)
	if err := refuseIneligibleResidentWorkerMove(database, task, cfg, toName, account); err != nil {
		return nil, AccountMoveResult{}, err
	}

	if runner != nil && runner.HasPendingRestart(task.ID) {
		return nil, AccountMoveResult{}, errAccountMovePending
	}
	sess := liveSession(runner, task.ID)
	if sess != nil && !sess.IsIdle() && !in.Force {
		return nil, AccountMoveResult{}, ErrAccountMoveBusy
	}
	var recycler backendMoveRunner
	if sess != nil {
		r, ok := runner.(backendMoveRunner)
		if !ok {
			return nil, AccountMoveResult{}, fmt.Errorf("this runner cannot restart a live session with a handoff")
		}
		recycler = r
	}

	// Build the seed BEFORE touching the row: it reads the Claude transcript
	// the old SessionID points at.
	original := originalPrompt(database, task)
	h := lastexchange.Handoff{
		From: BackendLabel(curKind), To: BackendLabel(toKind),
		Worktree: task.Worktree, Branch: task.Branch,
		OriginalPrompt: original,
	}
	if curKind == backendkind.Claude {
		h.Exchange = ClaudeLastExchange(task, cfg)
	}
	h.GitStatus, h.GitStatusKnown = handoffGitStatus(task.Worktree)
	seed := h.Prompt()

	newSession := ""
	if FamilyOf(to.Command).PinsSessionID {
		newSession = model.GenerateSessionID() // Claude pins; Codex captures after launch
	}
	routed := false // an explicit backend move is a pin
	prev, err := database.SetTaskBackendMove(task.ID, db.TaskBackendMove{
		Backend: toName, Account: storedAccount(account), AccountRouted: routed,
		SessionID: newSession, Prompt: seed,
	})
	if err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("persist backend move: %w", err)
	}
	if err := database.SetMeta(task.ID, backendMoveMetaNS, backendMoveMetaOrigPrompt, original); err != nil {
		slog.Warn("[backend-move] could not record original prompt", "task", task.ID, "err", err)
	}
	task.Backend, task.Model, task.Account, task.AccountRouted = toName, "", storedAccount(account), routed
	task.SessionID, task.Prompt = newSession, seed

	res := AccountMoveResult{
		From: NormalizeAccount(prev.Account), To: account,
		FromBackend: prevBackendName(task, prev), ToBackend: toName,
		Handoff: true,
	}
	logBackendMove(task.ID, res, fmt.Sprintf("persisted seed_bytes=%d", len(seed)))
	if sess == nil {
		return task, res, nil
	}

	rows, cols := in.Rows, in.Cols
	if rows == 0 || cols == 0 {
		c, r := sess.PTYSize()
		rows, cols = uint16(max(r, 0)), uint16(max(c, 0)) //nolint:gosec // PTY sizes are small positive ints
		if rows == 0 || cols == 0 {
			rows, cols = 24, 80
		}
	}
	restart := *task
	if err := recycler.Recycle(&restart, cfg, rows, cols); err != nil {
		if liveSession(runner, task.ID) == nil {
			logBackendMove(task.ID, res, fmt.Sprintf("session gone before restart (%v); caller starts it", err))
			return task, res, nil
		}
		if _, rbErr := database.SetTaskBackendMove(task.ID, prev); rbErr != nil {
			slog.Error("[backend-move] rollback failed", "task", task.ID, "err", rbErr)
		}
		uxlog.Log("[backend-move] task=%s %s→%s restart failed, rolled back: %v", task.ID, res.FromBackend, toName, err)
		return nil, AccountMoveResult{}, fmt.Errorf("restart on backend %q failed, task left on %q: %w", toName, res.FromBackend, err)
	}
	res.Started = true
	if err := database.SetStatus(task.ID, model.StatusInProgress); err != nil {
		return task, res, fmt.Errorf("persist restarted backend move status: %w", err)
	}
	task.SetStatus(model.StatusInProgress)
	logBackendMove(task.ID, res, "restarted in place with handoff")
	return task, res, nil
}

// refuseIneligibleResidentWorkerMove keeps a resident worker launchable: a move
// persists backend/account before any restart, and a STOPPED worker is never
// preflighted, so an ineligible target would strand it behind the launch gate
// on every later start/revive. The target must be an account enabled for
// resident workers and the moved task must pass the same unattended preflight
// the launch gate applies. Identity lookup errors fail closed. Non-resident
// tasks are untouched. backendName "" keeps the task's current backend.
func refuseIneligibleResidentWorkerMove(database *db.DB, task *model.Task, cfg config.Config, backendName, account string) error {
	worker, err := database.IsResidentWorker(task.ID)
	if err != nil {
		uxlog.Log("[resident] move refused task=%s: worker identity lookup failed: %v", task.ID, err)
		return fmt.Errorf("%w: resident worker identity: %w", ErrAccount, err)
	}
	if !worker {
		return nil
	}
	if cause := residentWorkerMoveCause(task, cfg, backendName, account); cause != nil {
		uxlog.Log("[resident] move refused task=%s backend=%q account=%q: %v", task.ID, backendName, account, cause)
		slog.Warn("[resident] move refused", "task", task.ID, "backend", backendName, "account", account, "err", cause)
		return fmt.Errorf("%w: resident workers can only move to an enabled account whose backend family supports unattended workers: %w", ErrAccount, cause)
	}
	return nil
}

// residentWorkerMoveCause reports why a resident worker could not run on
// backendName/account (nil when it can): the same rule spawn and the launch
// gate apply. Pure; it does not check worker identity.
func residentWorkerMoveCause(task *model.Task, cfg config.Config, backendName, account string) error {
	target := *task
	if backendName != "" {
		target.Backend, target.Model = backendName, ""
	}
	target.Account = storedAccount(account)
	if _, err := ResolveResidentWorker(cfg, task.Project, target.Backend, NormalizeAccount(account)); err != nil {
		return err
	}
	return preflightUnattended(&target, cfg)
}

// prevBackendName is the backend the task was on: the stored name, or the
// resolved default when none was stored.
func prevBackendName(task *model.Task, prev db.TaskBackendMove) string {
	if prev.Backend != "" {
		return prev.Backend
	}
	return "default"
}

func logBackendMove(taskID string, res AccountMoveResult, stage string) {
	uxlog.Log("[backend-move] task=%s %s→%s account=%s handoff=true: %s", taskID, res.FromBackend, res.ToBackend, res.To, stage)
	slog.Info("[backend-move]", "task", taskID, "from", res.FromBackend, "to", res.ToBackend, "account", res.To, "stage", stage)
}

// originalPrompt is the task's first prompt: the one saved before any earlier
// backend move rewrote Prompt with a seed, else the current Prompt.
func originalPrompt(database *db.DB, task *model.Task) string {
	if entries, err := database.ListMeta(task.ID, backendMoveMetaNS); err == nil {
		for _, e := range entries {
			if e.Key == backendMoveMetaOrigPrompt {
				return e.Value
			}
		}
	}
	return task.Prompt
}

// handoffGitStatus runs `git status --short` in the worktree. It fails open:
// ok=false means the status is unknown (not a clean tree).
func handoffGitStatus(worktree string) (string, bool) {
	if worktree == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), handoffGitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", worktree, "status", "--short").Output()
	if err != nil {
		slog.Warn("[backend-move] git status failed", "worktree", worktree, "err", err)
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

// ClaudeLastExchange extracts the user's last genuine message and the
// session's reply from task's Claude transcript. It never fails: every problem
// is logged and yields the zero Exchange. Call it before task.SessionID is
// cleared. The text stays local; it is only written into the new session's
// prompt.
func ClaudeLastExchange(task *model.Task, cfg config.Config) lastexchange.Exchange {
	if task == nil || task.SessionID == "" || task.Worktree == "" ||
		filepath.Base(task.SessionID) != task.SessionID {
		return lastexchange.Exchange{}
	}
	backend, err := ResolveBackend(task, cfg)
	if err != nil || !FamilyOf(backend.Command).SessionSwitcher {
		return lastexchange.Exchange{} // non-Claude backend: no transcript reader
	}
	dir, err := ClaudeConfigDirForTask(task, cfg)
	if err != nil {
		slog.Warn("[recycle] last exchange: config dir", "task", task.ID, "err", err)
		return lastexchange.Exchange{}
	}
	paths := []string{task.Worktree}
	if phys, perr := filepath.EvalSymlinks(task.Worktree); perr == nil && phys != task.Worktree {
		paths = append(paths, phys)
	}
	for _, p := range paths {
		proj, perr := claudesession.ProjectDirIn(dir, p)
		if perr != nil {
			continue
		}
		file := filepath.Join(proj, task.SessionID+".jsonl")
		if _, serr := os.Stat(file); serr != nil {
			continue
		}
		ex, rerr := lastexchange.FromFile(file)
		if rerr != nil {
			slog.Warn("[recycle] last exchange: read transcript", "task", task.ID, "err", rerr)
		}
		slog.Info("[recycle] last exchange extracted", "task", task.ID, "human_bytes", len(ex.Human), "reply_bytes", len(ex.Reply))
		return ex
	}
	slog.Info("[recycle] last exchange: transcript not found", "task", task.ID, "session", task.SessionID)
	return lastexchange.Exchange{}
}

// BackendNamesOfKind lists configured backend names whose command is of kind,
// sorted.
func BackendNamesOfKind(cfg config.Config, kind backendkind.Kind) []string {
	var out []string
	for name, b := range cfg.Backends {
		if backendkind.Of(b.Command) == kind {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
