package db

import (
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// SetHeraBoardItemDeferred changes human visibility without changing authored
// revision or read state. Only REST human controls call this mutation.
func (d *DB) SetHeraBoardItemDeferred(roleID, id int64, deferred bool, expected time.Time) (*HeraBoardItem, int, error) {
	if expected.IsZero() {
		return nil, 0, ErrHeraBoardInvalid
	}
	i, changed, err := d.setHeraBoardItemDeferred(roleID, id, deferred, expected)
	uxlog.Log("[board] defer role=%d item=%d deferred=%v changed=%v err=%v", roleID, id, deferred, changed, err)
	if err != nil {
		return nil, 0, err
	}
	if changed {
		d.emitBoardChanged(roleID, id, "deferred")
	}
	n, _, err := d.HeraBoardUnread(roleID)
	return i, n, err
}

func (d *DB) setHeraBoardItemDeferred(roleID, id int64, deferred bool, expected time.Time) (*HeraBoardItem, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i, err := scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
	if err != nil {
		return nil, false, err
	}
	if i.Resolved {
		return nil, false, ErrHeraBoardResolved
	}
	if formatTime(i.UpdatedAt) != formatTime(expected) {
		return nil, false, ErrHeraBoardConflict
	}
	if (i.DeferredAt != nil) == deferred {
		return i, false, nil
	}
	var stamp any
	if deferred {
		now := time.Now().UTC()
		i.DeferredAt = &now
		stamp = formatTime(now)
	} else {
		i.DeferredAt = nil
	}
	res, err := d.conn.Exec(`UPDATE hera_board_items SET deferred_at=? WHERE role_id=? AND id=? AND resolved=0 AND updated_at=?`, stamp, roleID, id, formatTime(expected))
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, ErrHeraBoardConflict
	}
	return i, true, nil
}
