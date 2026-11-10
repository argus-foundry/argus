package agent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ErrAccountMoveBusy rejects moving a task whose agent is actively working,
// unless the caller asked to force it.
var ErrAccountMoveBusy = errors.New("agent is working; pass force to interrupt it")

// errAccountMovePending rejects a move while another move or restart of the
// same task is still in flight; force does not override it. It wraps
// ErrAccountMoveBusy so callers map both to "try again later".
var errAccountMovePending = fmt.Errorf("%w (a move or restart of this task is already in progress)", ErrAccountMoveBusy)

// accountMovesInFlight serializes moves per task within this process; the
// runner's HasPendingRestart covers a restart queued by another process.
var accountMovesInFlight = struct {
	sync.Mutex
	ids map[string]bool
}{ids: map[string]bool{}}

func claimAccountMove(id string) bool {
	accountMovesInFlight.Lock()
	defer accountMovesInFlight.Unlock()
	if accountMovesInFlight.ids[id] {
		return false
	}
	accountMovesInFlight.ids[id] = true
	return true
}

func releaseAccountMove(id string) {
	accountMovesInFlight.Lock()
	delete(accountMovesInFlight.ids, id)
	accountMovesInFlight.Unlock()
}

// AccountMoveInFlight reports whether an account or backend move of id is in
// progress in this process. Graduation refuses while one is, which (with the
// move's own pending-graduate check, made after it claims the task) closes
// most of the move-vs-graduation race; the residual cross-lock window is a
// named follow-up (see gotchas/resident.md).
func AccountMoveInFlight(id string) bool {
	accountMovesInFlight.Lock()
	defer accountMovesInFlight.Unlock()
	return accountMovesInFlight.ids[id]
}

// refuseMoveOfPendingGraduate fails closed for a pending graduate: it restarts
// only through the daemon watcher, and an account move would kill+resume it
// alongside (account) or clear its session ID so the watcher's resume
// preflight fails forever (backend). Called after claimAccountMove so a
// concurrent graduation preflight sees the move as in flight.
func refuseMoveOfPendingGraduateImpl(database *db.DB, taskID string) error {
	if pending, perr := database.GraduationPendingErr(taskID); perr != nil || pending {
		uxlog.Log("[resident] refused account/backend move of task %s: graduation pending (check err=%v)", taskID, perr)
		return fmt.Errorf("%w (task %s)", db.ErrGraduationPending, taskID)
	}
	return nil
}

// refuseMoveOfPendingGraduate is a seam so a test can observe that the move
// is already claimed in flight when the check runs.
var refuseMoveOfPendingGraduate = refuseMoveOfPendingGraduateImpl

// AutoMoveTarget asks a move to pick the other account with the lowest usage.
const AutoMoveTarget = "auto"

// AccountMoveInput selects the task and the target account: a configured
// account, "default", or AutoMoveTarget. Force interrupts a busy session.
type AccountMoveInput struct {
	TaskID  string
	Account string
	Force   bool
	// Backend, when set, makes this a move to another agent CLI: a restart in
	// place with a handoff seed (MoveTaskBackend), not a resume.
	Backend string
	// Rows/Cols size the restarted PTY (0 → 24x80; the pane resizes it later).
	Rows, Cols uint16
}

// AccountMoveResult reports a completed move. Resumed is false when no
// transcript existed to carry over. Started is false when the task had no
// live session to restart in place; the caller then starts it through its own
// restart path.
type AccountMoveResult struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Resumed bool   `json:"resumed"`
	Started bool   `json:"started"`

	// Set only by a backend move (MoveTaskBackend), a restart with handoff.
	// ConversationCarried is always false: the conversation never comes along.
	FromBackend         string `json:"from_backend,omitempty"`
	ToBackend           string `json:"to_backend,omitempty"`
	Handoff             bool   `json:"handoff,omitempty"`
	ConversationCarried bool   `json:"conversation_carried,omitempty"`
}

// accountMoveRunner is the slice of a session runner a move needs: the live
// handle, and the in-place stop-and-relaunch the runner already uses for
// rerenders (it reserves a pending restart so exit handlers skip their
// terminal status flips, and carries the task's account to the supervisor).
type accountMoveRunner interface {
	Get(taskID string) SessionHandle
	KickRerender(task *model.Task, cfg config.Config, rows, cols uint16) error
	HasPendingRestart(taskID string) bool
}

// MoveTaskAccount moves a Claude task to another account without any model
// turn: it copies the task's newest transcript into the target account,
// switches and persists the task's account (a named target pins it, "auto" keeps it routable),
// and restarts a live session in place under the target with --resume. The
// task row, worktree, branch and hera bindings are untouched.
func MoveTaskAccount(database *db.DB, runner accountMoveRunner, in AccountMoveInput) (*model.Task, AccountMoveResult, error) {
	if strings.TrimSpace(in.Backend) != "" {
		return MoveTaskBackend(database, runner, in)
	}
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
		// Reject before the transcript copy: SetTaskAccount would refuse
		// later, leaving a copied transcript behind in the target account.
		return nil, AccountMoveResult{}, fmt.Errorf("%w: %w", ErrAccount, db.ErrResidentProtected)
	}
	cfg := database.Config()
	backend, err := ResolveBackend(task, cfg)
	if err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: %w", ErrAccount, err)
	}
	if backendkind.Of(backend.Command) != backendkind.Claude {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: only Claude tasks can move accounts (backend %q)", ErrAccount, task.Backend)
	}
	from := NormalizeAccount(task.Account)
	to, err := resolveMoveTarget(cfg, backend.Command, from, in.Account)
	if err != nil {
		return nil, AccountMoveResult{}, err
	}

	if runner != nil && runner.HasPendingRestart(task.ID) {
		return nil, AccountMoveResult{}, errAccountMovePending
	}
	sess := liveSession(runner, task.ID)
	if sess != nil && !sess.IsIdle() && !in.Force {
		return nil, AccountMoveResult{}, ErrAccountMoveBusy
	}

	RefreshResumeSessionID(database, task)
	srcDir, err := ClaudeConfigDirForTask(task, cfg)
	if err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: source account %q: %w", ErrAccount, from, err)
	}
	moved := *task
	moved.Account = storedAccount(to)
	// An auto move leaves the task routable like one created unpinned; a move
	// to a named account (or explicit "default") is a pin.
	routed := strings.TrimSpace(in.Account) == AutoMoveTarget
	moved.AccountRouted = routed
	if _, err := resolveSpawnClaudeConfigDir(&moved, cfg, true); err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: %w", ErrAccount, err)
	}
	dstDir, err := ClaudeConfigDirForTask(&moved, cfg)
	if err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: target account %q: %w", ErrAccount, to, err)
	}
	same := to == from
	if !same && filepath.Clean(srcDir) == filepath.Clean(dstDir) {
		// e.g. the backend pins CLAUDE_CONFIG_DIR through env_vars: switching
		// the stored account would not change the login at all.
		return nil, AccountMoveResult{}, fmt.Errorf("%w: accounts %q and %q resolve to the same Claude config dir for this backend", ErrAccount, from, to)
	}
	var resumed bool
	if same {
		// Re-picking the current account is a plain in-place restart: the
		// transcript is already in this account's dir, nothing to copy.
		resumed, _ = claudeResumeAvailable(task, cfg)
	} else if resumed, err = copyClaudeTranscript(srcDir, dstDir, task.Worktree, task.SessionID); err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("copy transcript to account %q: %w", to, err)
	}

	prevAccount, prevRouted := task.Account, task.AccountRouted
	if err := database.SetTaskAccount(task.ID, moved.Account, routed); err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("persist account move: %w", err)
	}
	task.Account, task.AccountRouted = moved.Account, routed
	res := AccountMoveResult{From: from, To: to, Resumed: resumed}
	logAccountMove(task.ID, res, "persisted")

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
	if resumed {
		err = runner.KickRerender(&restart, cfg, rows, cols)
	} else if reviver, ok := runner.(interface {
		Revive(*model.Task, config.Config, uint16, uint16) error
	}); ok {
		err = reviver.Revive(&restart, cfg, rows, cols)
	} else {
		err = runner.KickRerender(&restart, cfg, rows, cols)
	}
	if err != nil {
		if liveSession(runner, task.ID) == nil {
			// The session exited between the liveness check and the kick (the
			// daemon client flattens ErrSessionNotFound to a string, so check
			// liveness rather than the error value).
			logAccountMove(task.ID, res, fmt.Sprintf("session gone before restart (%v); caller starts it", err))
			return task, res, nil
		}
		// The old session is still running on the source account: put the
		// stored account back so the row never disagrees with the live agent.
		if rbErr := database.SetTaskAccount(task.ID, prevAccount, prevRouted); rbErr != nil {
			slog.Error("[account] move rollback failed", "task", task.ID, "err", rbErr)
		}
		uxlog.Log("[account] move task=%s %s→%s restart failed, account rolled back: %v", task.ID, from, to, err)
		slog.Warn("[account] move restart failed; rolled back", "task", task.ID, "from", from, "to", to, "err", err)
		return nil, AccountMoveResult{}, fmt.Errorf("restart on account %q failed, task left on %q: %w", to, from, err)
	}
	res.Started = true
	logAccountMove(task.ID, res, "restarted in place")
	return task, res, nil
}

// liveSession returns the task's session when it is alive, else nil.
func liveSession(runner accountMoveRunner, taskID string) SessionHandle {
	if runner == nil {
		return nil
	}
	if h := runner.Get(taskID); h != nil && h.Alive() {
		return h
	}
	return nil
}

// resolveMoveTarget validates the requested target and resolves "auto".
func resolveMoveTarget(cfg config.Config, backendCommand, from, requested string) (string, error) {
	target := strings.TrimSpace(requested)
	if target == "" {
		return "", fmt.Errorf("%w: account is required", ErrAccount)
	}
	if target == AutoMoveTarget {
		picked, scores, ok := PickMoveTarget(cfg, AccountUsage(cfg), backendCommand, from)
		if !ok {
			return "", fmt.Errorf("%w: no other account can run this task", ErrAccount)
		}
		uxlog.Log("[account] move: auto picked %s (scores %s)", picked, scores)
		return picked, nil
	}
	if err := cfg.ValidateAccount(target); err != nil {
		return "", fmt.Errorf("%w: %w", ErrAccount, err)
	}
	if !cfg.AccountSupports(target, backendCommand) {
		return "", fmt.Errorf("%w: account %q does not support this task's backend", ErrAccount, target)
	}
	return NormalizeAccount(target), nil
}

func logAccountMove(taskID string, res AccountMoveResult, stage string) {
	uxlog.Log("[account] move task=%s %s→%s resumed=%t: %s", taskID, res.From, res.To, res.Resumed, stage)
	slog.Info("[account] move", "task", taskID, "from", res.From, "to", res.To, "resumed", res.Resumed, "stage", stage)
}

// copyClaudeTranscript copies <sid>.jsonl and its <sid>/ sidecar dir from the
// source config dir's project folder into the destination's, for whichever of
// the logical / symlink-resolved worktree paths holds the transcript. Reports
// false (no error) when there is no transcript to copy.
func copyClaudeTranscript(srcDir, dstDir, worktree, sid string) (bool, error) {
	if worktree == "" || sid == "" || filepath.Base(sid) != sid {
		return false, nil
	}
	paths := []string{worktree}
	if physical, err := filepath.EvalSymlinks(worktree); err == nil && physical != worktree {
		paths = append(paths, physical)
	}
	for _, path := range paths {
		srcProj, err := claudesession.ProjectDirIn(srcDir, path)
		if err != nil {
			return false, err
		}
		src := filepath.Join(srcProj, sid+".jsonl")
		if info, err := os.Stat(src); err != nil || !info.Mode().IsRegular() {
			continue
		}
		dstProj, err := claudesession.ProjectDirIn(dstDir, path)
		if err != nil {
			return false, err
		}
		if err := os.MkdirAll(dstProj, 0o700); err != nil {
			return false, err
		}
		if err := copyFile(src, filepath.Join(dstProj, sid+".jsonl")); err != nil {
			return false, err
		}
		if info, err := os.Stat(filepath.Join(srcProj, sid)); err == nil && info.IsDir() {
			if err := copyTree(filepath.Join(srcProj, sid), filepath.Join(dstProj, sid)); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

// copyFile writes src to dst through a temp file + rename, so a reader never
// sees a half-written transcript and an older copy is replaced atomically.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".argus-move-*")
	if err != nil {
		return err
	}
	discard := func(err error) error {
		_ = os.Remove(tmp.Name()) // best effort: a stray temp file is harmless
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		return discard(err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return discard(err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return discard(err)
	}
	return nil
}

// copyTree copies regular files under src into dst, overwriting; symlinks and
// other special files are skipped.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			return copyFile(path, target)
		}
		return nil
	})
}
