package mcp

import (
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskMoveAccount_PendingGraduateIsToolError(t *testing.T) {
	s := testServer()
	s.SetAccountMover(func(agent.AccountMoveInput) (agent.AccountMoveResult, error) {
		return agent.AccountMoveResult{}, fmt.Errorf("%w (task w1)", db.ErrGraduationPending)
	})
	cr := callMove(t, s, `{"task_id":"w1","account":"auto"}`)
	testutil.True(t, cr.IsError)
	testutil.Contains(t, cr.Content[0].Text, "graduation restart pending")
}
