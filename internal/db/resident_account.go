package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
)

// Resident account-override meta keys (add-resident-account-switch).
const (
	MetaKeyAccountOverride = "account_override"
	MetaKeyLastAutoSwitch  = "last_auto_switch_at"
)

// Who chose a resident account override.
const (
	OverrideByManual = "manual"
	OverrideByAuto   = "auto"
)

// ResidentAccountOverride is a runtime account choice made by a resident move.
// It outranks resident.account until that setting changes (Base no longer
// matches) or the account becomes invalid. Account is the normalized name
// ("default" for the default account).
type ResidentAccountOverride struct {
	Account string `json:"account"`
	Base    string `json:"base"`
	By      string `json:"by"`
	At      string `json:"at"`
}

// ResidentAccountOverride reads the resident's override; ok is false when none
// is set or the stored value is unreadable (treated as absent).
func (d *DB) ResidentAccountOverride(taskID string) (ResidentAccountOverride, bool, error) {
	var raw string
	d.mu.Lock()
	err := d.conn.QueryRow(`SELECT value FROM task_meta WHERE task_id=? AND namespace=? AND key=?`,
		taskID, ResidentMetaNamespace, MetaKeyAccountOverride).Scan(&raw)
	d.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		return ResidentAccountOverride{}, false, nil
	}
	if err != nil {
		return ResidentAccountOverride{}, false, err
	}
	var ov ResidentAccountOverride
	if json.Unmarshal([]byte(raw), &ov) != nil || ov.Account == "" {
		return ResidentAccountOverride{}, false, nil
	}
	return ov, true, nil
}

// ClearResidentAccountOverride removes the override (a no-op when absent).
func (d *DB) ClearResidentAccountOverride(taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace=? AND key=?`,
		taskID, ResidentMetaNamespace, MetaKeyAccountOverride)
	return err
}

// SetResidentAccountMove is the only writer allowed past residentGuard for an
// account change: it writes the resident row's stored account and session id
// and the override meta in one transaction (and, for an automatic move, the
// last-auto-switch time). It refuses any row that is not the resident.
func (d *DB) SetResidentAccountMove(id, storedAccount, sessionID string, ov ResidentAccountOverride) error {
	if ov.At == "" {
		ov.At = time.Now().UTC().Format(ResidentTimeFormat)
	}
	val, err := json.Marshal(ov)
	if err != nil {
		return err
	}
	var accountChanged, sessionChanged bool
	err = d.WithTx(func(tx *sql.Tx) error {
		var kind, oldAccount, oldSession string
		if err := tx.QueryRow(`SELECT kind, account, session_id FROM tasks WHERE id=?`, id).Scan(&kind, &oldAccount, &oldSession); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
			}
			return err
		}
		if kind != model.KindResident {
			return fmt.Errorf("task %s is not the resident", id)
		}
		if _, err := tx.Exec(`UPDATE tasks SET account=?, account_routed=0, session_id=? WHERE id=?`, storedAccount, sessionID, id); err != nil {
			return err
		}
		now := formatTime(time.Now())
		upsert := `INSERT INTO task_meta (task_id, namespace, key, value, updated_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(task_id, namespace, key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`
		if _, err := tx.Exec(upsert, id, ResidentMetaNamespace, MetaKeyAccountOverride, string(val), now); err != nil {
			return err
		}
		if ov.By == OverrideByAuto {
			if _, err := tx.Exec(upsert, id, ResidentMetaNamespace, MetaKeyLastAutoSwitch, ov.At, now); err != nil {
				return err
			}
		}
		accountChanged, sessionChanged = oldAccount != storedAccount, oldSession != sessionID
		return nil
	})
	if err != nil {
		return err
	}
	if accountChanged {
		events.EmitTaskUpdated(id, model.TaskFieldAccount)
	}
	if sessionChanged {
		events.EmitTaskUpdated(id, model.TaskFieldRuntime)
	}
	return nil
}

// ResidentLastAutoSwitch returns the time of the last automatic account switch
// (zero when none), persisted so a daemon bounce does not reset the cooldown.
func (d *DB) ResidentLastAutoSwitch(taskID string) time.Time {
	var raw string
	d.mu.Lock()
	err := d.conn.QueryRow(`SELECT value FROM task_meta WHERE task_id=? AND namespace=? AND key=?`,
		taskID, ResidentMetaNamespace, MetaKeyLastAutoSwitch).Scan(&raw)
	d.mu.Unlock()
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(ResidentTimeFormat, raw)
	return t
}
