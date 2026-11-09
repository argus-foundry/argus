package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const initialPTYLayoutKey = "ui.initial_pty_layout"

// InitialPTYReportTTL prevents a closed viewer's geometry becoming a lasting
// launch default. Live viewers refresh at a shorter heartbeat.
const InitialPTYReportTTL = 2 * time.Minute

type PTYSize struct{ Rows, Cols uint16 }

func (s PTYSize) Valid() bool { return s.Rows >= 5 && s.Cols >= 20 }

// InitialPTYLayout is a launch preference, never a reconciled-history anchor.
type InitialPTYLayout struct {
	Active      PTYSize
	Coordinator PTYSize
	Worker      PTYSize
	// Resident is the resident tab's full-width pane. Absent (zero) from an
	// older TUI; InitialPTYSize then reports no size for the resident role
	// rather than substituting Active, which is whatever view the user is on.
	Resident   PTYSize
	ReportedAt time.Time
}

// InitialPTYRoleResident selects InitialPTYLayout.Resident in InitialPTYSize.
// It is a selector only, never a stored hera role kind.
const InitialPTYRoleResident HeraRoleKind = "resident"

func (d *DB) PublishInitialPTYLayout(layout InitialPTYLayout) error {
	if !layout.Active.Valid() || layout.ReportedAt.IsZero() {
		return fmt.Errorf("initial PTY layout requires a valid active pane and timestamp")
	}
	data, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	return d.SetConfigValue(initialPTYLayoutKey, string(data))
}

// InitialPTYSize returns zero when there is no recent usable viewer report.
// Callers retain their existing defaults and explicit-size precedence.
func (d *DB) InitialPTYSize(role HeraRoleKind, now time.Time) (PTYSize, error) {
	d.mu.Lock()
	var raw string
	err := d.conn.QueryRow("SELECT value FROM config WHERE key = ?", initialPTYLayoutKey).Scan(&raw)
	d.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		return PTYSize{}, nil
	}
	if err != nil {
		return PTYSize{}, err
	}
	var layout InitialPTYLayout
	if err := json.Unmarshal([]byte(raw), &layout); err != nil {
		return PTYSize{}, err
	}
	age := now.Sub(layout.ReportedAt)
	if age < 0 || age >= InitialPTYReportTTL || !layout.Active.Valid() {
		return PTYSize{}, nil
	}
	size := layout.Active
	switch role {
	case InitialPTYRoleResident:
		size = layout.Resident
	case HeraKindCoordinator:
		size = layout.Coordinator
	case HeraKindWorker, HeraKindFreelance:
		size = layout.Worker
	}
	if !size.Valid() {
		return PTYSize{}, nil
	}
	return size, nil
}
