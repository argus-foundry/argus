package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestArchiveMergeNotice(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "same text", "insert failure", "retire failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newUnitFixture(t)
			testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusInReview))
			if mode == "insert failure" {
				_, err := f.d.conn.Exec("CREATE TRIGGER reject_merge_marker BEFORE INSERT ON hera_merge_notices BEGIN SELECT RAISE(ABORT, 'marker failed'); END")
				testutil.NoError(t, err)
			}
			msg, err := f.d.SendHeraMergeNotice(f.task.ID, f.pc.ID, f.sub.ID, "merged", "merge notice")
			if mode == "insert failure" {
				testutil.Equal(t, err != nil, true)
				inbox, err := f.d.HeraInbox(f.sub.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, len(inbox), 0)
				return
			}
			testutil.NoError(t, err)
			testutil.NoError(t, f.d.MarkHeraMessageDelivered(msg.ID, HeraDeliveryIdleSubmit))
			testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusComplete))
			if mode == "retire failure" {
				_, err = f.d.conn.Exec("CREATE TRIGGER reject_retire BEFORE UPDATE OF read_at ON hera_messages BEGIN SELECT RAISE(ABORT, 'read failed'); END")
				testutil.NoError(t, err)
			}
			if mode == "same text" {
				_, err = f.d.SendHeraMessage(f.pc.ID, f.sub.ID, "merged", "merge notice", nil)
				testutil.NoError(t, err)
			}
			n, err := f.d.HeraArchiveUnread(f.sub.ID)
			testutil.NoError(t, err)
			want := 0
			if mode == "real mail" || mode == "same text" {
				want = 1
			}
			testutil.Equal(t, n, want)
			input := ArchiveUnitInput{RoleID: f.sub.ID}
			if mode == "rollback" {
				input.PreCommit = func(string) error { return errors.New("busy") }
			}
			_, err = f.d.ArchiveHeraUnit(input)
			testutil.Equal(t, err == nil, mode == "commit")
			msgs, err := f.d.HeraMessagesByIDs([]int64{msg.ID})
			testutil.NoError(t, err)
			testutil.Equal(t, msgs[0].ReadAt != nil, mode == "commit")
			if mode != "commit" {
				f.assertUntouched(t)
			}
		})
	}
}

func TestMergeNoticeSkipsFinishedTask(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "archived"}[archived], func(t *testing.T) {
			f := newUnitFixture(t)
			if archived {
				testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusInReview))
				testutil.NoError(t, f.d.SetArchived(f.task.ID, true))
			}
			msg, err := f.d.SendHeraMergeNotice(f.task.ID, f.pc.ID, f.sub.ID, "merged", "merge notice")
			testutil.NoError(t, err)
			testutil.Nil(t, msg)
			inbox, err := f.d.HeraInbox(f.sub.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(inbox), 0)
		})
	}
}

func TestMergeNoticeFailsClosed(t *testing.T) {
	for _, state := range []string{"missing task", "wrong task", "invalid message", "archived role", "unread lookup"} {
		t.Run(state, func(t *testing.T) {
			f := newUnitFixture(t)
			testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusInReview))
			id, body := f.task.ID, "merged"
			switch state {
			case "missing task":
				id = "missing"
			case "wrong task":
				other := &model.Task{Name: "other", Project: "p", Status: model.StatusInProgress}
				testutil.NoError(t, f.d.Add(other))
				id = other.ID
			case "invalid message":
				body = strings.Repeat("x", model.MaxMessageBodyBytes+1)
			case "archived role":
				testutil.NoError(t, f.d.ArchiveHeraRole(f.sub.ID))
			case "unread lookup":
				_, err := f.d.conn.Exec("DROP TABLE hera_merge_notices")
				testutil.NoError(t, err)
				_, err = f.d.HeraArchiveUnread(f.sub.ID)
				testutil.Equal(t, err != nil, true)
				testutil.NoError(t, f.d.SetStatus(f.task.ID, model.StatusComplete))
				_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
				testutil.Contains(t, err.Error(), "inbox unreadable")
				f.assertUntouched(t)
				return
			}
			msg, err := f.d.SendHeraMergeNotice(id, f.pc.ID, f.sub.ID, body, "merge notice")
			testutil.Equal(t, err != nil, true)
			testutil.Nil(t, msg)
			inbox, err := f.d.HeraInbox(f.sub.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(inbox), 0)
		})
	}
}
