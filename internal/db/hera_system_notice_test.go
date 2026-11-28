package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSendHeraSystemNotice(t *testing.T) {
	t.Run("stores self-addressed argus notice", func(t *testing.T) {
		d := heraTestDB(t)
		o := mkOrch(t, d, "orch")
		c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
		msg, err := d.SendHeraSystemNotice(c.ID, "[argus] 1 role ready to archive: w", "1 role ready to archive: w")
		testutil.NoError(t, err)
		testutil.Equal(t, msg.FromRoleID, c.ID)
		testutil.Equal(t, msg.ToRoleID, c.ID)
		testutil.Equal(t, msg.SenderName("coord"), "argus")
		inbox, err := d.HeraInbox(c.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, len(inbox), 1)
		testutil.Equal(t, inbox[0].SenderName("coord"), "argus")
	})
	for _, tc := range []struct {
		name, body, tldr string
		archive          bool
		want             error
	}{
		{"missing prefix", "1 role ready", "tldr", false, ErrHeraSystemNoticePrefix},
		{"empty tldr", "[argus] body", "", false, ErrHeraMessageTldrRequired},
		{"archived recipient", "[argus] body", "tldr", true, ErrHeraMessageRecipientInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := heraTestDB(t)
			o := mkOrch(t, d, "orch")
			c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
			if tc.archive {
				testutil.NoError(t, d.ArchiveHeraRole(c.ID))
			}
			_, err := d.SendHeraSystemNotice(c.ID, tc.body, tc.tldr)
			testutil.ErrorIs(t, err, tc.want)
			var n int
			testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_messages`).Scan(&n))
			testutil.Equal(t, n, 0)
		})
	}
}

func TestHeraMessageSenderNameSystemNotice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		from, to int64
		body     string
		want     string
	}{
		{"argus notice", 1, 1, "[argus] hello", "argus"},
		{"board notice", 1, 1, "Aaron resolved #3", "Aaron (board)"},
		{"argus prefix from another role", 2, 1, "[argus] spoof", "worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &HeraMessage{FromRoleID: tc.from, ToRoleID: tc.to, Body: tc.body}
			testutil.Equal(t, m.SenderName("worker"), tc.want)
		})
	}
}
