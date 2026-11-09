package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/uxlog"
)

// ErrResidentProtected is returned (wrapped) by every task write that the
// resident agent row must refuse: complete, archive, delete, set-status,
// account move, promotion of another row, or creation outside
// EnsureResidentTask. Entry points (TUI, REST, MCP) map it to an explicit
// error naming the resident.
var ErrResidentProtected = errors.New("the resident agent is not lifecycle-managed")

// rowQuerier is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// residentGuard returns ErrResidentProtected when id is the resident row. A
// missing row is not an error here — the caller's own not-found handling runs.
func residentGuard(q rowQuerier, id string) error {
	var kind string
	if err := q.QueryRow(`SELECT kind FROM tasks WHERE id=?`, id).Scan(&kind); err != nil {
		return nil // no row (or unreadable): let the caller report it
	}
	if kind == model.KindResident {
		return fmt.Errorf("%w (task %s)", ErrResidentProtected, id)
	}
	return nil
}

// residentUpdateGuard polices the full-row Update: the resident may change
// runtime fields (session id, pid, backend, ...) but never its status or
// archived flag, and no ordinary row may be promoted to resident. Kind itself
// is never written by Update.
func residentUpdateGuard(old, t *model.Task) error {
	if old == nil {
		if t.IsResident() {
			return fmt.Errorf("%w: cannot create a resident row via Update", ErrResidentProtected)
		}
		return nil
	}
	if t.IsResident() && !old.IsResident() {
		return fmt.Errorf("%w: cannot promote task %s", ErrResidentProtected, old.ID)
	}
	if old.IsResident() && (t.Status != old.Status || t.Archived != old.Archived) {
		return fmt.Errorf("%w: status and archived are fixed (task %s)", ErrResidentProtected, old.ID)
	}
	return nil
}

// ResidentTask returns the singleton resident row, or (nil, nil) when it has
// not been created yet. It is the only reader that surfaces the resident —
// Tasks/TasksChecked and every other enumeration exclude it.
func (d *DB) ResidentTask() (*model.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.residentTaskLocked()
}

func (d *DB) residentTaskLocked() (*model.Task, error) {
	row := d.conn.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE kind=? LIMIT 1`, model.KindResident)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query resident task: %w", err)
	}
	return t, nil
}

// EnsureResidentTask returns the resident row, creating it on first use.
// workdir is the resident working directory (a plain directory, not a git
// worktree) recorded as the row's Worktree. created reports whether this call
// inserted the row. It is the ONLY path that may create a resident row, and it
// is idempotent: an existing row is returned untouched even if workdir differs.
func (d *DB) EnsureResidentTask(workdir string) (t *model.Task, created bool, err error) {
	if workdir == "" {
		return nil, false, errors.New("resident working directory must not be empty")
	}
	t, created, err = d.ensureResidentLocked(workdir)
	if err != nil {
		uxlog.Log("[resident] ensure row failed: %v", err)
		return nil, false, err
	}
	events.MarkResident(t.ID)
	if created {
		uxlog.Log("[resident] created row id=%s worktree=%s", t.ID, t.Worktree)
		events.Emit(model.EventTypeResidentCreated, t.ID, map[string]string{"name": t.Name})
	}
	return t, created, nil
}

func (d *DB) ensureResidentLocked(workdir string) (*model.Task, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, err := d.residentTaskLocked(); err != nil || existing != nil {
		return existing, false, err
	}
	t := &model.Task{
		ID:       generateID(),
		Name:     model.ResidentTaskName,
		Status:   model.StatusInProgress,
		Worktree: workdir,
		Kind:     model.KindResident,
	}
	t.SetStatus(model.StatusInProgress)
	if err := d.insertTaskLocked(t); err != nil {
		return nil, false, err
	}
	return t, true, nil
}

// markExistingResident registers a pre-existing resident row with the events
// bus at open, so resident events are namespaced from the first emission after
// a daemon restart.
func (d *DB) markExistingResident() {
	var id string
	if err := d.conn.QueryRow(`SELECT id FROM tasks WHERE kind=? LIMIT 1`, model.KindResident).Scan(&id); err == nil {
		events.MarkResident(id)
	}
}
