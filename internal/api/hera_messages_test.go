package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/uxlog"
)

type msgPage struct {
	Messages []struct {
		ID           int64      `json:"id"`
		FromRoleID   int64      `json:"from_role_id"`
		ToRoleID     int64      `json:"to_role_id"`
		InReplyTo    *int64     `json:"in_reply_to"`
		SentAt       time.Time  `json:"sent_at"`
		DeliveryMode string     `json:"delivery_mode"`
		DeliveredAt  *time.Time `json:"delivered_at"`
		Source       string     `json:"source"`
		FromName     string     `json:"from_name"`
		ToName       string     `json:"to_name"`
		Body         string     `json:"body"`
		Tldr         string     `json:"tldr"`
		ReadAt       *time.Time `json:"read_at"`
	} `json:"messages"`
	Next *int64 `json:"next_before_id"`
}

func getMsgs(t *testing.T, s *Server, path string) (*httptest.ResponseRecorder, msgPage) {
	t.Helper()
	rr := doBoard(s, "GET", path, "")
	var p msgPage
	if rr.Code == 200 {
		testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &p))
	}
	return rr, p
}

func TestHeraMessagesHistory(t *testing.T) {
	s, d := testServer(t)
	task := &model.Task{Name: "t", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	o, err := d.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	coord, _ := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	w1, _ := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w1", Kind: db.HeraKindWorker})
	w2, _ := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w2", Kind: db.HeraKindWorker})
	other, _ := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "other", Kind: db.HeraKindWorker})
	b1, err := d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: w1.ID, ArgusTaskID: task.ID, WorktreePath: "/a"})
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(b1.ID, "moved"))
	_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: w2.ID, ArgusTaskID: task.ID, WorktreePath: "/b"})
	testutil.NoError(t, err)

	base := "/api/tasks/" + task.ID + "/hera-messages"

	t.Run("empty history", func(t *testing.T) {
		rr, p := getMsgs(t, s, base)
		testutil.Equal(t, rr.Code, 200)
		testutil.Equal(t, len(p.Messages), 0)
		testutil.Contains(t, rr.Body.String(), `"messages":[]`)
	})

	for _, role := range []*db.HeraRole{w1, w2, other} {
		testutil.NoError(t, d.SetHeraTreeCursor(role.ID, 12345+role.ID))
	}
	var ids []int64
	for i := 0; i < 5; i++ {
		to := w1.ID
		if i%2 == 1 {
			to = w2.ID
		}
		m, err := d.SendHeraMessage(coord.ID, to, fmt.Sprintf("b%d", i), "t", nil)
		testutil.NoError(t, err)
		ids = append(ids, m.ID)
	}
	_, err = d.SendHeraMessage(coord.ID, other.ID, "unrelated", "t", nil)
	testutil.NoError(t, err)
	_, err = d.SendHeraMessage(w2.ID, coord.ID, "to coord", "t", nil)
	testutil.NoError(t, err)
	unreadBefore, _ := d.HeraInbox(w2.ID)
	otherBefore, _ := d.HeraInbox(other.ID)

	t.Run("pages oldest first with cursor across ended and active bindings", func(t *testing.T) {
		rr, p := getMsgs(t, s, base+"?limit=2")
		testutil.Equal(t, rr.Code, 200)
		testutil.Equal(t, len(p.Messages), 2)
		testutil.Equal(t, p.Messages[0].ID, ids[3])
		testutil.Equal(t, p.Messages[1].ID, ids[4])
		testutil.Equal(t, p.Messages[0].Source, "hera")
		testutil.Equal(t, p.Messages[0].FromName, "coord")
		testutil.Equal(t, p.Messages[0].ToName, "w2")
		testutil.Equal(t, *p.Next, ids[3])
		_, p2 := getMsgs(t, s, fmt.Sprintf("%s?limit=2&before_id=%d", base, *p.Next))
		testutil.Equal(t, p2.Messages[0].ID, ids[1])
		_, p3 := getMsgs(t, s, fmt.Sprintf("%s?limit=2&before_id=%d", base, *p2.Next))
		testutil.Equal(t, len(p3.Messages), 1)
		testutil.Equal(t, p3.Messages[0].ID, ids[0])
		testutil.Nil(t, p3.Next)
	})
	t.Run("excludes unrelated mail and does not consume", func(t *testing.T) {
		_, p := getMsgs(t, s, base)
		testutil.Equal(t, len(p.Messages), 5)
		for _, m := range p.Messages {
			testutil.Nil(t, m.ReadAt)
			if m.Body == "unrelated" {
				t.Fatal("unrelated mail leaked")
			}
		}
		after, _ := d.HeraInbox(w2.ID)
		testutil.Equal(t, len(after), len(unreadBefore))
		otherAfter, _ := d.HeraInbox(other.ID)
		testutil.Equal(t, len(otherAfter), len(otherBefore))
	})
	for _, role := range []*db.HeraRole{w1, w2, other} {
		cursor, err := d.GetHeraTreeCursor(role.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, cursor, 12345+role.ID)
	}
	t.Run("sanitizes terminal controls", func(t *testing.T) {
		m, err := d.SendHeraMessage(other.ID, w2.ID, "hi\x1b[31m\nline", "tl\x1bdr", &ids[0])
		testutil.NoError(t, err)
		_, p := getMsgs(t, s, base+"?limit=1")
		testutil.Equal(t, p.Messages[0].ID, m.ID)
		testutil.Equal(t, p.Messages[0].FromRoleID, other.ID)
		testutil.Equal(t, p.Messages[0].ToRoleID, w2.ID)
		testutil.Equal(t, p.Messages[0].ToName, "w2")
		testutil.Equal(t, *p.Messages[0].InReplyTo, ids[0])
		testutil.Equal(t, p.Messages[0].SentAt.Equal(m.SentAt), true)
		testutil.Equal(t, p.Messages[0].DeliveryMode, db.HeraDeliveryPending)
		testutil.Nil(t, p.Messages[0].DeliveredAt)
		testutil.Equal(t, p.Messages[0].FromName, "other")
		testutil.Equal(t, p.Messages[0].Body, "hi[31m\nline")
		testutil.Equal(t, p.Messages[0].Tldr, "tldr")
	})
	t.Run("validation and status codes", func(t *testing.T) {
		for _, q := range []string{"?limit=0", "?limit=501", "?limit=x", "?before_id=0", "?before_id=-3", "?before_id=x"} {
			rr, _ := getMsgs(t, s, base+q)
			testutil.Equal(t, rr.Code, 400)
		}
		rr, _ := getMsgs(t, s, "/api/tasks/nope/hera-messages")
		testutil.Equal(t, rr.Code, 404)
		rr = httptest.NewRecorder()
		unauthed(s).ServeHTTP(rr, httptest.NewRequest("GET", base, nil))
		testutil.Equal(t, rr.Code, 401)
	})
	t.Run("store failure is 500", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "ux.log")
		testutil.NoError(t, uxlog.Init(logPath))
		t.Cleanup(uxlog.Close)
		testutil.NoError(t, d.Close())
		rr, _ := getMsgs(t, s, base)
		testutil.Equal(t, rr.Code, 500)
		testutil.Equal(t, rr.Body.String(), "{\"error\":\"internal server error\"}\n")
		log, err := os.ReadFile(logPath)
		testutil.NoError(t, err)
		testutil.Contains(t, string(log), "[hera-messages] REST task lookup failed task=")
		testutil.Contains(t, string(log), "sql: database is closed")
	})
}
