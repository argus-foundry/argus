package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/apiclient"
	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/tui/modal"
	"github.com/argus-foundry/argus/internal/usagebudget"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// remoteAccountMover is satisfied by *apistore.Store: in --remote mode the
// daemon performs the move and the restart.
type remoteAccountMover interface {
	MoveTaskAccount(ctx context.Context, id, account string, force bool) (*apiclient.MoveAccountResp, error)
}

// remoteBackendMover is the handoff variant, also satisfied by *apistore.Store.
type remoteBackendMover interface {
	MoveTaskBackend(ctx context.Context, id, backend, account string, force bool) (*apiclient.MoveAccountResp, error)
}

// kickRunner is the runner surface agent.MoveTaskAccount restarts a live
// session through; both the daemon client and the in-process runner have it.
type kickRunner interface {
	Get(taskID string) agent.SessionHandle
	KickRerender(task *model.Task, cfg config.Config, rows, cols uint16) error
	Recycle(task *model.Task, cfg config.Config, rows, cols uint16) error
	HasPendingRestart(taskID string) bool
}

// openAccountPicker starts the `>` flow for taskID. Everything that can block
// (the task read, which is HTTP in remote mode; the session-status and usage
// reads, which are daemon RPCs) runs off the UI thread; the picker, or a
// notice when the task cannot move, is shown back on it.
func (a *App) openAccountPicker(taskID string) {
	if a.accountPicker != nil || taskID == "" {
		return
	}
	go func() {
		t, err := a.db.Get(taskID)
		if err != nil || t == nil {
			a.tapp.QueueUpdateDraw(func() { a.flashNotice("Move account: task not found") })
			return
		}
		busy := false
		if h := a.runner.Get(taskID); h != nil && h.Alive() && !h.IsIdle() {
			busy = true
		}
		choices, notice := a.accountMoveChoices(t)
		a.tapp.QueueUpdateDraw(func() {
			if notice != "" {
				uxlog.Log("[account] picker task=%s not shown: %s", t.ID, notice)
				a.flashNotice(notice)
				return
			}
			a.showAccountPicker(t, choices, busy)
		})
	}()
}

// accountMoveChoices lists "auto" plus every account that can run the task
// (the current one marked), labelled with usage, then one handoff row per
// backend and account of the OTHER CLI (Claude <-> Codex). A non-empty notice
// means the task cannot move.
func (a *App) accountMoveChoices(t *model.Task) ([]modal.AccountChoice, string) {
	cfg := a.db.Config()
	current := agent.NormalizeAccount(t.Account)
	backend, err := agent.ResolveBackend(t, cfg)
	cur := backendkind.Claude
	if err == nil {
		cur = backendkind.Of(backend.Command)
	}
	if cur != backendkind.Claude && cur != backendkind.Codex {
		return nil, "Only Claude and Codex tasks can move"
	}
	if _, local := a.db.(*db.DB); !local {
		if rl, ok := a.db.(remoteAccountLister); ok {
			return remoteAccountMoveChoices(rl, current, cfg, cur)
		}
	}
	usage := agent.AccountUsage(cfg)
	now := time.Now()
	var rows []modal.AccountChoice
	if names := cfg.AccountNamesFor("claude"); cur == backendkind.Claude && len(names) >= 2 {
		var all []modal.AccountChoice
		for _, n := range names {
			five, week := usagebudget.LiveWindows(usage[n], now)
			all = append(all, accountChoice(n, n == current, windowPct(five), windowPct(week)))
		}
		auto := modal.AccountChoice{Name: agent.AutoMoveTarget, Detail: autoDetail}
		if picked, _, ok := agent.PickMoveTarget(cfg, usage, "claude", current); ok {
			auto.Detail = autoDetail + " (now " + picked + ")"
		}
		rows = append([]modal.AccountChoice{auto}, all...)
	}
	if cur == backendkind.Codex {
		for _, n := range cfg.AccountNamesFor("claude") {
			five, week := usagebudget.LiveWindows(usage[n], now)
			rows = append(rows, handoffChoices(cfg, cur, n, windowPct(five), windowPct(week))...)
		}
	} else {
		for _, n := range cfg.AccountNamesFor("codex") {
			rows = append(rows, handoffChoices(cfg, cur, n, nil, codexWeeklyPct(n))...)
		}
	}
	if len(rows) == 0 {
		return nil, noMoveNotice
	}
	return rows, ""
}

const noMoveNotice = "No other account or CLI can run this task"

// Handoff picker text. The label says it is a restart, never a resume; the
// note names what the other CLI loses.
const (
	handoffDetail    = "restart with handoff (conversation not carried over)"
	handoffNoteCodex = "Codex loses: bridge mod + context indicator, exact needs-input, hera_new_orchestrator"
	handoffNoteClaud = "Claude: the Codex conversation is not carried over; a handoff seeds the new session"
)

// handoffTarget encodes a handoff picker selection as "@backend:account".
func handoffTarget(backend, account string) string { return "@" + backend + ":" + account }

// parseHandoffTarget decodes handoffTarget; ok is false for an ordinary
// account selection.
func parseHandoffTarget(sel string) (backend, account string, ok bool) {
	rest, found := strings.CutPrefix(sel, "@")
	if !found {
		return "", "", false
	}
	backend, account, found = strings.Cut(rest, ":")
	return backend, account, found && backend != "" && account != ""
}

// handoffChoices builds the handoff rows for one account of the CLI opposite
// to cur: one per configured backend of that CLI. Usage windows are optional.
func handoffChoices(cfg config.Config, cur backendkind.Kind, account string, five, week *float64) []modal.AccountChoice {
	target := backendkind.Codex
	note := handoffNoteCodex
	if cur == backendkind.Codex {
		target, note = backendkind.Claude, handoffNoteClaud
	}
	var out []modal.AccountChoice
	for _, name := range agent.BackendNamesOfKind(cfg, target) {
		if !cfg.AccountSupports(account, cfg.Backends[name].Command) {
			continue
		}
		detail := handoffDetail
		if five != nil || week != nil {
			detail += " · " + usageDetail(five, week)
			for _, p := range []*float64{five, week} {
				if p != nil && *p >= nearLimitPct {
					detail += " ⚠ near limit"
					break
				}
			}
		}
		out = append(out, modal.AccountChoice{
			Name:   handoffTarget(name, account),
			Label:  agent.BackendLabel(target) + " · " + account,
			Detail: detail,
			Note:   note,
		})
	}
	return out
}

// codexWeeklyPct is the cached Codex weekly usage, shown on the default
// account's row only (Codex reports one ambient reading). Nil when unknown.
func codexWeeklyPct(account string) *float64 {
	if account != config.DefaultAccountName {
		return nil
	}
	r, ok := backendtier.CachedReading()
	if !ok {
		return nil
	}
	p := r.Percentage
	return &p
}

// nearLimitPct is the usage at or above which the picker warns on an account.
// The account stays selectable.
const nearLimitPct = 80.0

// accountChoice builds one picker row: usage, a "current" marker for the
// account the task is on (re-picking it restarts the task in place), and a
// warning when either window is at or near its limit.
func accountChoice(name string, current bool, five, week *float64) modal.AccountChoice {
	detail := usageDetail(five, week)
	for _, p := range []*float64{five, week} {
		if p != nil && *p >= nearLimitPct {
			detail += " ⚠ near limit"
			break
		}
	}
	if current {
		detail += " (current, restarts in place)"
	}
	return modal.AccountChoice{Name: name, Detail: detail}
}

// remoteAccountMoveChoices builds the choices from the remote daemon's
// GET /api/accounts, which carries the usage readings. cfg supplies the
// backend names for the handoff rows.
func remoteAccountMoveChoices(rl remoteAccountLister, current string, cfg config.Config, cur backendkind.Kind) ([]modal.AccountChoice, string) {
	list, err := rl.Accounts(context.Background())
	if err != nil {
		return nil, "Move account: could not list accounts: " + err.Error()
	}
	var all, handoff []modal.AccountChoice
	for _, ac := range list {
		var five, week *float64
		if u := ac.Usage; u != nil {
			five, week = u.FiveHourPct, u.WeeklyPct
		}
		if ac.Supports.Claude {
			if cur == backendkind.Claude {
				all = append(all, accountChoice(ac.Name, ac.Name == current, five, week))
			} else {
				handoff = append(handoff, handoffChoices(cfg, cur, ac.Name, five, week)...)
			}
		}
		if ac.Supports.Codex && cur == backendkind.Claude {
			handoff = append(handoff, handoffChoices(cfg, cur, ac.Name, nil, nil)...)
		}
	}
	var rows []modal.AccountChoice
	if len(all) >= 2 {
		rows = append([]modal.AccountChoice{{Name: agent.AutoMoveTarget, Detail: autoDetail}}, all...)
	}
	rows = append(rows, handoff...)
	if len(rows) == 0 {
		return nil, noMoveNotice
	}
	return rows, ""
}

// autoDetail describes the "auto" choice in the picker.
const autoDetail = "lowest usage"

func windowPct(w *usagebudget.Window) *float64 {
	if w == nil {
		return nil
	}
	p := w.Percentage
	return &p
}

// usageDetail renders "5h 100% · wk 75%", with "—" for an unknown window.
func usageDetail(fiveHour, weekly *float64) string {
	f := func(p *float64) string {
		if p == nil {
			return "—"
		}
		return fmt.Sprintf("%.0f%%", *p)
	}
	return "5h " + f(fiveHour) + " · wk " + f(weekly)
}

func (a *App) showAccountPicker(t *model.Task, choices []modal.AccountChoice, busy bool) {
	if a.accountPicker != nil {
		return
	}
	warning := ""
	if busy {
		warning = "The agent is working; moving will interrupt it."
	}
	a.accountPickerPrevPage, _ = a.pages.GetFrontPage()
	a.accountPickerTask = t
	a.accountPickerForce = busy
	a.accountPicker = modal.NewAccountPickerModal(fmt.Sprintf("Move %s (on %s) to account", t.Name, agent.NormalizeAccount(t.Account)), warning, choices)
	a.mode = modeAccountPicker
	a.pages.AddPage("accountpicker", a.accountPicker, true, true)
	a.pages.SwitchToPage("accountpicker")
	a.tapp.SetFocus(a.accountPicker)
	uxlog.Log("[account] picker open task=%s choices=%d busy=%t", t.ID, len(choices), busy)
}

func (a *App) handleAccountPickerKey(event *tcell.EventKey) {
	a.accountPicker.InputHandler()(event, func(tview.Primitive) {})
	if a.accountPicker.Canceled() {
		uxlog.Log("[account] picker canceled task=%s", a.accountPickerTask.ID)
		a.closeAccountPicker()
		return
	}
	if target := a.accountPicker.Selected(); target != "" {
		t, force := a.accountPickerTask, a.accountPickerForce
		a.closeAccountPicker()
		a.runAccountMove(t, target, force)
	}
}

func (a *App) closeAccountPicker() {
	a.mode = modeTaskList
	a.accountPicker = nil
	a.accountPickerTask = nil
	a.accountPickerForce = false
	a.pages.RemovePage("accountpicker")
	prev := a.accountPickerPrevPage
	a.accountPickerPrevPage = ""
	if prev == "accountpicker" {
		prev = ""
	}
	a.restorePageFocus(prev)
}

// runAccountMove performs the move off the UI thread. Locally a live session
// restarts in place through the runner; an exited one is started here through
// startSession. Remotely the daemon does both.
func (a *App) runAccountMove(t *model.Task, target string, force bool) {
	if a.graduationPendingBlocked(t.ID, "account move") {
		return
	}
	toBackend, toAccount, handoff := parseHandoffTarget(target)
	if handoff {
		a.statusbar.SetInfo(fmt.Sprintf("Moving %s to %s (%s) with handoff…", t.Name, toBackend, toAccount))
		uxlog.Log("[backend-move] requested task=%s backend=%s account=%s force=%t", t.ID, toBackend, toAccount, force)
	} else {
		a.statusbar.SetInfo(fmt.Sprintf("Moving %s to %s…", t.Name, target))
		uxlog.Log("[account] move requested task=%s target=%s force=%t", t.ID, target, force)
	}
	if _, local := a.db.(*db.DB); !local {
		rm, ok := a.db.(remoteAccountMover)
		bm, bok := a.db.(remoteBackendMover)
		if !ok || (handoff && !bok) {
			a.statusbar.SetError("Move account is not available in this mode")
			return
		}
		go func() {
			var resp *apiclient.MoveAccountResp
			var err error
			if handoff {
				resp, err = bm.MoveTaskBackend(context.Background(), t.ID, toBackend, toAccount, force)
			} else {
				resp, err = rm.MoveTaskAccount(context.Background(), t.ID, target, force)
			}
			a.tapp.QueueUpdateDraw(func() {
				if err != nil {
					a.accountMoveFailed(t, err)
					return
				}
				a.accountMoveDone(t, agent.AccountMoveResult{From: resp.From, To: resp.To, Resumed: resp.Resumed, Started: resp.Started,
					FromBackend: resp.FromBackend, ToBackend: resp.ToBackend, Handoff: resp.Handoff})
			})
		}()
		return
	}
	d := a.db.(*db.DB)
	rows, cols := a.spawnPTYSize(t.ID)
	// A runner without in-place restart passes nil: the move then reports
	// Started=false and the session is started below.
	runner, _ := a.runner.(kickRunner)
	go func() {
		in := agent.AccountMoveInput{TaskID: t.ID, Account: target, Force: force, Rows: rows, Cols: cols}
		if handoff {
			in.Backend, in.Account = toBackend, toAccount
		}
		_, res, err := agent.MoveTaskAccount(d, runner, in)
		a.tapp.QueueUpdateDraw(func() {
			a.invalidateTasksChangeGate()
			if err != nil {
				a.accountMoveFailed(t, err)
				return
			}
			if !res.Started {
				fresh, gerr := a.db.Get(t.ID)
				if gerr != nil || fresh == nil {
					a.accountMoveFailed(t, fmt.Errorf("reload task: %w", gerr))
					return
				}
				a.startSession(fresh)
			}
			a.accountMoveDone(t, res)
		})
	}()
}

func (a *App) accountMoveFailed(t *model.Task, err error) {
	uxlog.Log("[account] move failed task=%s: %v", t.ID, err)
	if graduationPendingErr(err) {
		a.statusbar.ClearInfo()
		a.flashNotice(db.ErrGraduationPending.Error())
		return
	}
	msg := err.Error()
	if i := strings.Index(msg, ": "); strings.HasPrefix(msg, agent.ErrAccount.Error()+": ") && i >= 0 {
		msg = msg[i+2:]
	}
	a.statusbar.SetError("Move account failed: " + msg)
}

func (a *App) accountMoveDone(t *model.Task, res agent.AccountMoveResult) {
	if res.Handoff {
		uxlog.Log("[backend-move] done task=%s %s→%s account=%s", t.ID, res.FromBackend, res.ToBackend, res.To)
		a.statusbar.ClearInfo()
		a.flashNotice(fmt.Sprintf("Moved %s to %s with handoff (conversation not carried over)", t.Name, res.ToBackend))
		a.refreshTasks()
		return
	}
	note := fmt.Sprintf("Moved %s: %s → %s", t.Name, res.From, res.To)
	if !res.Resumed {
		note += " (no transcript; started fresh)"
	}
	uxlog.Log("[account] move done task=%s %s→%s resumed=%t", t.ID, res.From, res.To, res.Resumed)
	a.statusbar.ClearInfo()
	a.flashNotice(note)
	a.refreshTasks()
}
