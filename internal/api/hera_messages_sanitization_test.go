package api

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraMessagesHistoryNameControlsAndTabs(t *testing.T) {
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
	message, err := s.db.SendHeraMessage(from.ID, to.ID, "one\ttwo\nnext", "short\twords", nil)
	testutil.NoError(t, err)
	_, err = conn.Exec(`UPDATE hera_roles SET name=? WHERE id=?`, "from\x1b\tworker", from.ID)
	testutil.NoError(t, err)
	_, err = conn.Exec(`UPDATE hera_roles SET name=? WHERE id=?`, "to\x07\tworker", to.ID)
	testutil.NoError(t, err)
	rr, page := getMsgs(t, s, path)
	testutil.Equal(t, rr.Code, 200)
	testutil.Equal(t, len(page.Messages), 1)
	if len(page.Messages) != 1 {
		return
	}
	testutil.Equal(t, page.Messages[0].ID, message.ID)
	testutil.Equal(t, page.Messages[0].FromName, "from worker")
	testutil.Equal(t, page.Messages[0].ToName, "to worker")
	testutil.Equal(t, page.Messages[0].Body, "one two\nnext")
	testutil.Equal(t, page.Messages[0].Tldr, "short words")
}
