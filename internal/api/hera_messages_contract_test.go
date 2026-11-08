package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/uxlog"
)

func historyFileFixture(t *testing.T) (*Server, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.sql")
	d, err := db.Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(on)")
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	task := &model.Task{Name: "history", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	return New(d, agent.NewRunner(nil), "test-token", nil, nil), conn, "/api/tasks/" + task.ID + "/hera-messages"
}

func TestHeraMessagesHistoryQueryFailure(t *testing.T) {
	s, conn, path := historyFileFixture(t)
	_, err := conn.Exec(`DROP TABLE hera_messages`)
	testutil.NoError(t, err)
	logPath := filepath.Join(t.TempDir(), "ux.log")
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	rr, _ := getMsgs(t, s, path)
	testutil.Equal(t, rr.Code, 500)
	testutil.Equal(t, rr.Body.String(), "{\"error\":\"internal server error\"}\n")
	log, err := os.ReadFile(logPath)
	testutil.NoError(t, err)
	testutil.Contains(t, string(log), "[hera-messages] REST history failed task=")
	testutil.Contains(t, string(log), "no such table: hera_messages")
}

func TestHeraMessagesHistoryDeletedNameFallback(t *testing.T) {
	s, conn, path := historyFileFixture(t)
	o, err := s.db.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	from, err := s.db.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "from", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	to, err := s.db.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "to", Kind: db.HeraKindWorker})
	testutil.NoError(t, err)
	taskID := path[len("/api/tasks/") : len(path)-len("/hera-messages")]
	_, err = s.db.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: to.ID, ArgusTaskID: taskID, WorktreePath: "/wt"})
	testutil.NoError(t, err)
	m, err := s.db.SendHeraMessage(from.ID, to.ID, "body", "tldr", nil)
	testutil.NoError(t, err)
	testutil.NoError(t, s.db.MarkHeraMessageDelivered(m.ID, db.HeraDeliveryIdleSubmit))
	_, err = s.db.MarkHeraMessagesRead(to.ID, []int64{m.ID})
	testutil.NoError(t, err)
	stored, err := s.db.HeraMessagesByIDs([]int64{m.ID})
	testutil.NoError(t, err)
	// Real deletion cascades mail. Renaming preserves foreign keys and mail while making name lookups unavailable.
	_, err = conn.Exec(`ALTER TABLE hera_roles RENAME TO unavailable_roles`)
	testutil.NoError(t, err)
	rr, p := getMsgs(t, s, path)
	testutil.Equal(t, rr.Code, 200)
	testutil.Equal(t, len(p.Messages), 1)
	testutil.Equal(t, p.Messages[0].ID, m.ID)
	testutil.Equal(t, p.Messages[0].FromName, "(deleted role)")
	testutil.Equal(t, p.Messages[0].ToName, "(deleted role)")
	testutil.Equal(t, p.Messages[0].DeliveryMode, db.HeraDeliveryIdleSubmit)
	testutil.Equal(t, p.Messages[0].DeliveredAt.Equal(*stored[0].DeliveredAt), true)
	testutil.Equal(t, p.Messages[0].ReadAt.Equal(*stored[0].ReadAt), true)
}

func TestHeraMessagesHistoryDefaultLimit(t *testing.T) {
	s, d := testServer(t)
	task := &model.Task{Name: "history", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	o, err := d.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	from, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "from", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	to, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "to", Kind: db.HeraKindWorker})
	testutil.NoError(t, err)
	_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: to.ID, ArgusTaskID: task.ID, WorktreePath: "/wt"})
	testutil.NoError(t, err)
	var ids []int64
	for index := range 101 {
		if index > 0 && index%50 == 0 {
			from, err = d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: fmt.Sprintf("sender%d", index), Kind: db.HeraKindWorker})
			testutil.NoError(t, err)
		}
		m, err := d.SendHeraMessage(from.ID, to.ID, "body", "tldr", nil)
		testutil.NoError(t, err)
		ids = append(ids, m.ID)
	}
	rr, p := getMsgs(t, s, "/api/tasks/"+task.ID+"/hera-messages")
	testutil.Equal(t, rr.Code, 200)
	testutil.Equal(t, len(p.Messages), 100)
	testutil.Equal(t, p.Messages[0].ID, ids[1])
	testutil.Equal(t, p.Messages[99].ID, ids[100])
	testutil.Equal(t, *p.Next, ids[1])
}

func TestHeraMessagesHistorySuccessLog(t *testing.T) {
	s, conn, path := historyFileFixture(t)
	o, err := s.db.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	from, err := s.db.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "from", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	to, err := s.db.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "to", Kind: db.HeraKindWorker})
	testutil.NoError(t, err)
	taskID := path[len("/api/tasks/") : len(path)-len("/hera-messages")]
	_, err = s.db.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: to.ID, ArgusTaskID: taskID, WorktreePath: "/wt"})
	testutil.NoError(t, err)
	_, err = conn.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at) VALUES(?,?,?,'private tldr','2026-01-01T00:00:00Z'),(?,?,?,'private tldr','2026-01-01T00:00:00Z')`, from.ID, to.ID, "private body", from.ID, to.ID, "private body")
	testutil.NoError(t, err)
	logPath := filepath.Join(t.TempDir(), "ux.log")
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	rr, p := getMsgs(t, s, path+"?limit=1")
	testutil.Equal(t, rr.Code, 200)
	testutil.Equal(t, len(p.Messages), 1)
	rr, p = getMsgs(t, s, fmt.Sprintf("%s?limit=1&before_id=%d", path, *p.Next))
	testutil.Equal(t, rr.Code, 200)
	testutil.Equal(t, len(p.Messages), 1)
	log, err := os.ReadFile(logPath)
	testutil.NoError(t, err)
	testutil.Contains(t, string(log), "[hera-messages] REST history task="+taskID+" count=1 more=true")
	testutil.Contains(t, string(log), "[hera-messages] REST history task="+taskID+" count=1 more=false")
	testutil.Equal(t, strings.Contains(string(log), "private body"), false)
	testutil.Equal(t, strings.Contains(string(log), "private tldr"), false)
}
