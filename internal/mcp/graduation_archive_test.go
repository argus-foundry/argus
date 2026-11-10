package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// gradPendingTaskDB refuses archiving writes the way *db.DB does while a
// graduate's restart is pending.
type gradPendingTaskDB struct{ *mockTaskDB }

func (g *gradPendingTaskDB) Update(t *model.Task) error {
	if t.Archived {
		return fmt.Errorf("%w (task %s)", db.ErrGraduationPending, t.ID)
	}
	return g.mockTaskDB.Update(t)
}

func TestToolArchive_GraduationPendingIsToolError(t *testing.T) {
	s, base, stopper := testServerWithTasks()
	s.SetTaskManager(func(TaskCreateInput) (*model.Task, error) { return nil, nil }, &gradPendingTaskDB{base}, stopper)
	resp := doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "task_archive",
		Arguments: json.RawMessage(`{"id":"abc123","archived":true}`),
	})
	testutil.NoError(t, respErr(resp))
	cr := callResult(t, resp)
	testutil.True(t, cr.IsError)
	testutil.Contains(t, textOf(cr), "graduation restart pending")
}
