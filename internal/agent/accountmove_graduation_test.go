package agent

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

// A pending graduate restarts only through the daemon watcher: an account or
// backend move must refuse before copying, persisting or kicking anything.
func TestMoveTaskAccount_RefusesPendingGraduate(t *testing.T) {
	for _, tc := range []struct{ name, backend string }{{"account", ""}, {"backend", "codex"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFixture(t, true)
			testutil.NoError(t, f.d.SetMeta("mv", db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
			r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
			_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Account: "work", Backend: tc.backend})
			testutil.True(t, errors.Is(err, db.ErrGraduationPending))
			testutil.Nil(t, r.kicked)
			testutil.Nil(t, r.revived)
			got, gerr := f.d.Get("mv")
			testutil.NoError(t, gerr)
			testutil.Equal(t, got.Account, "")
			testutil.Equal(t, got.SessionID, "sid-1")
			testutil.Equal(t, got.Backend, "test")
		})
	}
	t.Run("control: not pending moves", func(t *testing.T) {
		f := newMoveFixture(t, true)
		r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
		_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Account: "work"})
		testutil.NoError(t, err)
		testutil.NotNil(t, r.kicked)
	})
}

// An unreadable pending marker must fail closed: refuse, never move.
func TestMoveTaskAccount_UnreadableMarkerFailsClosed(t *testing.T) {
	f := newMoveFixture(t, true)
	testutil.NoError(t, f.d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TABLE task_meta`)
		return err
	}))
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Account: "work"})
	testutil.True(t, errors.Is(err, db.ErrGraduationPending))
	testutil.Nil(t, r.kicked)
	got, gerr := f.d.Get("mv")
	testutil.NoError(t, gerr)
	testutil.Equal(t, got.Account, "")
}
