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

	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/usagebudget"
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
// only through the daemon watcher, and any account or backend move would
// restart it alongside the watcher (a kill+resume) or clear/re-mint its
// session ID (no transferable transcript, or a backend move), so the watcher's
// resume preflight fails forever. Called after claimAccountMove so a
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

// ErrAccountMoveRestartUnknown means the persisted move may still start.
var ErrAccountMoveRestartUnknown = errors.New("account move restart outcome unknown")

// SessionTransfer copies a task's saved conversation from the from-account's
// storage into the to-account's (copies of the task that differ only in
// Account) and reports whether a conversation was carried over. It validates
// the target, sets the destination conversation identity, and leaves the source untouched.
type SessionTransfer func(from, to *model.Task, cfg config.Config) (resumed bool, err error)

// AccountMoveSupported reports whether the backend command's family can move
// accounts while preserving the conversation.
func AccountMoveSupported(command string) bool {
	return FamilyOf(command).TransferSession != nil
}

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

// MoveTaskAccount moves a task to another account without any model
// turn: it copies the task's saved conversation into the target account,
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
	transfer := FamilyOf(backend.Command).TransferSession
	if transfer == nil {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: backend %q cannot move accounts", ErrAccount, task.Backend)
	}
	from := NormalizeAccount(task.Account)
	// A resident worker's auto pick must only consider accounts it could
	// actually run on: the lowest-usage account is often one disabled for
	// resident workers, and picking it first would refuse a move that an
	// enabled account could have satisfied.
	var eligible func(string) bool
	if worker, werr := database.IsResidentWorker(task.ID); werr != nil {
		uxlog.Log("[resident] move refused task=%s: worker identity lookup failed: %v", task.ID, werr)
		return nil, AccountMoveResult{}, fmt.Errorf("%w: resident worker identity: %w", ErrAccount, werr)
	} else if worker {
		eligible = func(name string) bool {
			return residentWorkerMoveCause(task, cfg, task.Backend, name) == nil
		}
	}
	to, err := resolveMoveTarget(cfg, backend.Command, from, in.Account, eligible)
	if err != nil {
		return nil, AccountMoveResult{}, err
	}
	if err := refuseIneligibleResidentWorkerMove(database, task, cfg, task.Backend, to); err != nil {
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
	moved := *task
	moved.Account = storedAccount(to)
	// An auto move leaves the task routable like one created unpinned; a move
	// to a named account (or explicit "default") is a pin.
	routed := strings.TrimSpace(in.Account) == AutoMoveTarget
	moved.AccountRouted = routed
	resumed, err := transfer(task, &moved, cfg)
	if err != nil {
		return nil, AccountMoveResult{}, err
	}

	if !resumed && sess != nil && task.SessionID == "" && !FamilyOf(backend.Command).PinsSessionID {
		return nil, AccountMoveResult{}, fmt.Errorf("%w: cannot identify the running conversation; stop the task before moving it", ErrAccount)
	}

	var recycler backendMoveRunner
	if !resumed && sess != nil {
		var ok bool
		recycler, ok = runner.(backendMoveRunner)
		if !ok {
			return nil, AccountMoveResult{}, fmt.Errorf("%w: runner cannot start a fresh conversation", ErrAccount)
		}
	}
	if !resumed {
		moved.SessionID = ""
		if FamilyOf(backend.Command).PinsSessionID {
			moved.SessionID = model.GenerateSessionID()
		}
	}
	if err := database.SetTaskAccountMove(task.ID, moved.Account, routed, moved.SessionID); err != nil {
		return nil, AccountMoveResult{}, fmt.Errorf("persist account move: %w", err)
	}
	task.Account, task.AccountRouted, task.SessionID = moved.Account, routed, moved.SessionID
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
	} else {
		err = recycler.Recycle(&restart, cfg, rows, cols)
	}

	if err != nil {
		logAccountMove(task.ID, res, fmt.Sprintf("restart outcome unknown; keeping account: %v", err))
		return task, res, fmt.Errorf("%w: %w", ErrAccountMoveRestartUnknown, err)
	}
	res.Started = true
	if err := database.SetStatus(task.ID, model.StatusInProgress); err != nil {
		return task, res, fmt.Errorf("persist restarted account move status: %w", err)
	}
	task.SetStatus(model.StatusInProgress)
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
// A non-nil eligible restricts the auto candidates to accounts it accepts.
func resolveMoveTarget(cfg config.Config, backendCommand, from, requested string, eligible func(string) bool) (string, error) {
	target := strings.TrimSpace(requested)
	if target == "" {
		return "", fmt.Errorf("%w: account is required", ErrAccount)
	}
	if target == AutoMoveTarget {
		picked, scores, ok := pickMoveTargetWhere(cfg, AccountUsage(cfg), backendCommand, from, eligible)
		if !ok {
			if eligible != nil {
				uxlog.Log("[resident] auto move refused: no other account is eligible for a resident worker")
				return "", fmt.Errorf("%w: resident workers can only move to an enabled account whose backend family supports unattended workers (no other eligible account)", ErrAccount)
			}
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

// PreviewMoveTarget is the account an "auto" move of task would pick right now
// (the picker's "now X" label), applying the same resident-worker eligibility
// filter MoveTaskAccount does. Fail-closed: when worker identity cannot be
// determined, ok is false rather than naming an account the move might refuse.
func PreviewMoveTarget(database *db.DB, task *model.Task, cfg config.Config, usage map[string]usagebudget.Reading, backendCommand, exclude string) (string, bool) {
	var keep func(string) bool
	if database != nil && task != nil {
		worker, err := database.IsResidentWorker(task.ID)
		if err != nil {
			uxlog.Log("[resident] move preview: worker identity lookup failed task=%s: %v", task.ID, err)
			return "", false
		}
		if worker {
			keep = func(name string) bool { return residentWorkerMoveCause(task, cfg, task.Backend, name) == nil }
		}
	}
	picked, _, ok := pickMoveTargetWhere(cfg, usage, backendCommand, exclude, keep)
	return picked, ok
}

func claudeTransferSession(from, to *model.Task, cfg config.Config) (bool, error) {
	fromName, toName := NormalizeAccount(from.Account), NormalizeAccount(to.Account)
	if fromName == toName {
		// Re-picking the current account is a plain in-place restart: the
		// transcript is already in this account's dir, nothing to copy.
		resumed, _ := claudeResumeAvailable(from, cfg)
		return resumed, nil
	}
	srcDir, err := ClaudeConfigDirForTask(from, cfg)
	if err != nil {
		return false, fmt.Errorf("%w: source account %q: %w", ErrAccount, fromName, err)
	}
	if _, err := resolveSpawnClaudeConfigDir(to, cfg, true); err != nil {
		return false, fmt.Errorf("%w: %w", ErrAccount, err)
	}
	dstDir, err := ClaudeConfigDirForTask(to, cfg)
	if err != nil {
		return false, fmt.Errorf("%w: target account %q: %w", ErrAccount, toName, err)
	}
	if filepath.Clean(srcDir) == filepath.Clean(dstDir) {
		// e.g. the backend pins CLAUDE_CONFIG_DIR through env_vars: switching
		// the stored account would not change the login at all.
		return false, fmt.Errorf("%w: accounts %q and %q resolve to the same Claude config dir for this backend", ErrAccount, fromName, toName)
	}
	resumed, err := copyClaudeTranscript(srcDir, dstDir, from.Worktree, from.SessionID)
	if err != nil {
		return false, fmt.Errorf("copy transcript to account %q: %w", toName, err)
	}
	return resumed, nil
}
