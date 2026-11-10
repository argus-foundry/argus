package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

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
	w, _, err := e.db.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "alpha", Kind: db.HeraKindWorker, ArgusProject: "p"}, "wt", t.TempDir())
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
	e.m.sendNotice = func(from, to int64, body, tldr string) error {
		if fail {
			return errors.New("inbox full")
		}
		sent = append(sent, tldr)
		_, err := e.db.SendHeraMessage(from, to, body, tldr, nil)
		return err
	}
	e.m.deliverNotices()
	testutil.Equal(t, len(sent), 0)
	left, err := e.db.PendingResidentNotices(10)
	testutil.NoError(t, err)
	testutil.Equal(t, len(left), 2) // released, not lost
	fail = false
	e.m.deliverNotices()
	testutil.Equal(t, len(sent), 2)
	testutil.True(t, strings.HasPrefix(sent[0], "Worker renamed"))
	testutil.True(t, strings.HasPrefix(sent[1], "Worker beta was hidden"))
}

func TestResidentNotice_RecipientGoneDropsNotice(t *testing.T) {
	e := newNoticeEnv(t)
	testutil.NoError(t, e.db.NukeHeraRole(e.worker.ID))
	e.m.sendNotice = func(int64, int64, string, string) error { return db.ErrHeraMessageRecipientInvalid }
	e.m.deliverNotices()
	left, err := e.db.PendingResidentNotices(10)
	testutil.NoError(t, err)
	testutil.Equal(t, len(left), 0)
}
