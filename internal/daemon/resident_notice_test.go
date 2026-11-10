package daemon

import (
	"database/sql"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type noticeEnv struct {
	*resEnv
	coord  *db.HeraRole
	worker *db.HeraRole
}

func newNoticeEnv(t *testing.T) *noticeEnv {
	t.Helper()
	e := newResEnv(t)
	task, _, err := e.db.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := e.db.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, e.db.Add(&model.Task{ID: "wt", Name: "alpha-task", Status: model.StatusInProgress, Project: "p", Branch: "argus/resident/alpha", CreatedAt: time.Now()}))
	w, _, err := e.db.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "alpha", Kind: db.HeraKindWorker, ArgusProject: "p"}, "wt", t.TempDir())
	testutil.NoError(t, err)
	roles, err := e.db.ListHeraRoles(o.ID, true)
	testutil.NoError(t, err)
	var coord *db.HeraRole
	for _, r := range roles {
		if r.Kind == db.HeraKindCoordinator {
			coord = r
		}
	}
	testutil.True(t, coord != nil)
	return &noticeEnv{resEnv: e, coord: coord, worker: w}
}

func (e *noticeEnv) inbox(t *testing.T) []*db.HeraMessage {
	t.Helper()
	msgs, err := e.db.HeraInbox(e.coord.ID)
	testutil.NoError(t, err)
	return msgs
}

func TestResidentNotice_DeliveredOncePerHumanAction(t *testing.T) {
	cases := []struct {
		name string
		act  func(e *noticeEnv) error
		want []string
	}{
		{"nuke", func(e *noticeEnv) error { return e.db.NukeHeraRole(e.worker.ID) }, []string{"nuked", "alpha", "argus/resident/alpha"}},
		{"hide", func(e *noticeEnv) error { return e.db.ArchiveHeraRole(e.worker.ID) }, []string{"hid", "alpha", "session was stopped"}},
		{"rename", func(e *noticeEnv) error { return e.db.RenameHeraRole(e.worker.ID, "beta") }, []string{"renamed", "alpha → beta"}},
		{"tasks-list delete", func(e *noticeEnv) error { return e.db.Delete("wt") }, []string{"nuked", "alpha", "Tasks list"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newNoticeEnv(t)
			testutil.NoError(t, tc.act(e))
			e.m.deliverNotices()
			e.m.deliverNotices() // a second pass must not duplicate
			msgs := e.inbox(t)
			testutil.Equal(t, len(msgs), 1)
			text := msgs[0].Tldr + "\n" + msgs[0].Body
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("message missing %q:\n%s", w, text)
				}
			}
			testutil.Equal(t, msgs[0].ToRoleID, e.coord.ID)
			left, err := e.db.PendingResidentNotices(10)
			testutil.NoError(t, err)
			testutil.Equal(t, len(left), 0)
		})
	}
}

func TestResidentNotice_ResidentsOwnDeleteAndStatusStepsAreSilent(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.UpsertHeraRoleStatus(e.worker.ID, db.HeraRoleStatusValue("working")))
	testutil.NoError(t, e.db.NukeHeraRoleAs(e.worker.ID, db.HeraActorResident))
	e.m.deliverNotices()
	testutil.Equal(t, len(e.inbox(t)), 0)
}

func TestResidentNotice_FailedSendIsRetriedInOrder(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.RenameHeraRole(e.worker.ID, "beta"))
	testutil.NoError(t, e.db.ArchiveHeraRole(e.worker.ID))
	fail := true
	var sent []string
	e.m.commitNotice = func(id, from, to int64, body, tldr string) (*db.HeraMessage, error) {
		if fail {
			return nil, db.ErrHeraMessageInboxFull
		}
		sent = append(sent, tldr)
		return e.db.DeliverResidentNotice(id, from, to, body, tldr)
	}
	e.m.deliverNotices()
	testutil.Equal(t, len(sent), 0)
	left, err := e.db.PendingResidentNotices(10)
	testutil.NoError(t, err)
	testutil.Equal(t, len(left), 2) // still queued, not lost
	fail = false
	e.m.deliverNotices()
	testutil.Equal(t, len(sent), 2)
	testutil.True(t, strings.HasPrefix(sent[0], "Worker renamed"))
	testutil.True(t, strings.HasPrefix(sent[1], "Worker beta was hidden"))
}

func TestResidentNotice_RecipientGoneDropsNotice(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.NukeHeraRole(e.worker.ID))
	e.m.commitNotice = func(int64, int64, int64, string, string) (*db.HeraMessage, error) {
		return nil, db.ErrHeraMessageRecipientInvalid
	}
	e.m.deliverNotices()
	left, err := e.db.PendingResidentNotices(10)
	testutil.NoError(t, err)
	testutil.Equal(t, len(left), 0)
}

func TestResidentNotice_LongNamesDoNotPoisonQueue(t *testing.T) {
	long1 := strings.Repeat("a", 60)
	long2 := strings.Repeat("é", 30) // 60 bytes, multibyte
	cases := []struct {
		name string
		acts []func(e *noticeEnv) error
	}{
		{"rename then hide", []func(e *noticeEnv) error{
			func(e *noticeEnv) error { return e.db.RenameHeraRole(e.worker.ID, long2) },
			func(e *noticeEnv) error { return e.db.ArchiveHeraRole(e.worker.ID) },
		}},
		{"rename then nuke", []func(e *noticeEnv) error{
			func(e *noticeEnv) error { return e.db.RenameHeraRole(e.worker.ID, long2) },
			func(e *noticeEnv) error { return e.db.NukeHeraRole(e.worker.ID) },
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newNoticeEnv(t)
			testutil.NoError(t, e.db.RenameHeraRole(e.worker.ID, long1))
			for _, a := range tc.acts {
				testutil.NoError(t, a(e))
			}
			e.m.deliverNotices()
			left, err := e.db.PendingResidentNotices(10)
			testutil.NoError(t, err)
			testutil.Equal(t, len(left), 0)
			msgs := e.inbox(t)
			testutil.Equal(t, len(msgs), 3)
			for _, m := range msgs {
				testutil.True(t, len(m.Tldr) <= db.HeraMaxTldrLen)
				testutil.False(t, strings.ContainsAny(m.Tldr, "\r\n"))
			}
			testutil.Contains(t, msgs[0].Body, "alpha → "+long1)
			testutil.True(t, strings.Contains(msgs[1].Body, long1+" → "+long2))
		})
	}
}

func TestBoundedTldr(t *testing.T) {
	testutil.Equal(t, boundedTldr("a\nb\r\nc"), "a b c")
	got := boundedTldr(strings.Repeat("é", 100))
	testutil.True(t, len(got) <= db.HeraMaxTldrLen)
	testutil.True(t, strings.HasSuffix(got, "…"))
	testutil.True(t, utf8.ValidString(got))
}

func TestResidentNotice_PermanentErrorDoesNotWedgeQueue(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.RenameHeraRole(e.worker.ID, "beta"))
	testutil.NoError(t, e.db.ArchiveHeraRole(e.worker.ID))
	calls := 0
	e.m.commitNotice = func(id, from, to int64, body, tldr string) (*db.HeraMessage, error) {
		calls++
		if calls == 1 {
			return nil, db.ErrHeraMessageTldrTooLong
		}
		return e.db.DeliverResidentNotice(id, from, to, body, tldr)
	}
	e.m.deliverNotices()
	testutil.Equal(t, len(e.inbox(t)), 1) // the bad row was dropped, the next delivered
	left, _ := e.db.PendingResidentNotices(10)
	testutil.Equal(t, len(left), 0)
}

func TestResidentNotice_LegacyClaimedRowIsDelivered(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.NukeHeraRole(e.worker.ID))
	// Simulate a row stranded by the retired claim protocol.
	testutil.NoError(t, e.db.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE resident_notices SET claimed_at='2026-01-01T00:00:00Z'`)
		return err
	}))
	e.m.deliverNotices()
	testutil.Equal(t, len(e.inbox(t)), 1)
}

func TestResidentNotice_CrashAfterCommitBeforeDoorbellNoDuplicate(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.NukeHeraRole(e.worker.ID))
	// The daemon dies after the commit and before ringing: the ring seam never runs.
	rings := 0
	e.m.ringNotice = func(*db.HeraMessage) { rings++ }
	e.m.commitNotice = func(id, from, to int64, body, tldr string) (*db.HeraMessage, error) {
		if _, err := e.db.DeliverResidentNotice(id, from, to, body, tldr); err != nil {
			return nil, err
		}
		panic("crash") // before the caller can ring
	}
	func() {
		defer func() { _ = recover() }()
		e.m.deliverNotices()
	}()
	testutil.Equal(t, rings, 0)
	left, err := e.db.PendingResidentNotices(10)
	testutil.NoError(t, err)
	testutil.Equal(t, len(left), 0)       // notice is gone...
	testutil.Equal(t, len(e.inbox(t)), 1) // ...and exactly one message exists
	e.m.commitNotice = nil
	e.m.deliverNotices() // a later pass must not duplicate
	testutil.Equal(t, len(e.inbox(t)), 1)
}

func TestResidentNotice_RingsDoorbellForCommittedMessage(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.NukeHeraRole(e.worker.ID))
	var rung []*db.HeraMessage
	e.m.ringNotice = func(m *db.HeraMessage) { rung = append(rung, m) }
	e.m.deliverNotices()
	testutil.Equal(t, len(rung), 1)
	testutil.Equal(t, rung[0].ToRoleID, e.coord.ID)
}
