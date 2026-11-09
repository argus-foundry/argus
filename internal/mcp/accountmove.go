package mcp

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/argus-foundry/argus/internal/agent"
)

// AccountMover performs an account move plus restart. The daemon injects
// (*api.Server).MoveTaskAccount; internal/mcp cannot import internal/api.
type AccountMover func(agent.AccountMoveInput) (agent.AccountMoveResult, error)

// accountMoveToolDefs are exposed only when SetAccountMover has been called.
var accountMoveToolDefs = []Tool{
	{
		Name: "task_move_account",
		Description: `Move an Argus task to another account and resume its conversation there (Claude backends), or to another CLI by a restart with handoff (see the backend parameter). Use it to rescue an agent stuck on its account's usage limit when another account has budget. It copies the conversation into the target account, switches the task's account, and restarts the same task in place (same worktree, branch and hera role); no new task is created.

` + "`account`" + ` is a configured account name, "default", or "auto" (the other account with the lowest current usage). Naming the account the task is already on restarts it in place there. Check each account's 5-hour and weekly usage first with argus_api_get on /api/accounts. To move a task to ANOTHER CLI instead (for example when every Claude account is out of budget), pass ` + "`backend`" + ` (a configured Codex backend name; Codex to Claude works the same way) and optionally ` + "`account`" + ` (default "default"; "auto" is not supported). That is a restart in place with a handoff, NOT a resume: the conversation does not carry over. The new session is told so and is given the worktree, branch, git status, the last user exchange (Claude source only, when readable) and the original prompt. Moving to Codex loses: the argus bridge mod and context indicator, accurate needs-input detection (it falls back to screen scraping), and hera_new_orchestrator (rejected for non-Claude callers). The result carries from_backend, to_backend, handoff and conversation_carried=false.

A task whose agent is actively working is refused unless ` + "`force`" + ` is true, which interrupts it. A task cannot move itself (the restart would end the session waiting for the answer): pass your own ` + "`caller_id`" + ` or ` + "`cwd`" + `, and ask your coordinator or the user to move you instead.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id":   map[string]interface{}{"type": "string", "description": "ID of the task to move. Required."},
				"account":   map[string]interface{}{"type": "string", "description": `Target account: a configured name, "default", or "auto". Required unless backend is set (then it defaults to "default").`},
				"backend":   map[string]interface{}{"type": "string", "description": "Optional. A configured backend of the other CLI (Claude to Codex or back): restart in place with a handoff instead of a resume."},
				"force":     map[string]interface{}{"type": "boolean", "description": "Interrupt the agent if it is actively working. Default false."},
				"caller_id": map[string]interface{}{"type": "string", "description": "Your own task ID (ARGUS_TASK_ID), used to refuse a self-move."},
				"cwd":       map[string]interface{}{"type": "string", "description": "Your working directory, used to refuse a self-move when caller_id is omitted."},
			},
			"required": []string{"task_id"},
		},
	},
}

type accountMoverSlot struct {
	mu   sync.RWMutex
	move AccountMover
}

// SetAccountMover wires the task_move_account tool. Safe to call after the
// MCP listener is serving (the daemon starts MCP before the REST API).
func (s *Server) SetAccountMover(m AccountMover) {
	s.accountMover.mu.Lock()
	s.accountMover.move = m
	s.accountMover.mu.Unlock()
}

func (s *Server) accountMoverFn() AccountMover {
	s.accountMover.mu.RLock()
	defer s.accountMover.mu.RUnlock()
	return s.accountMover.move
}

func (s *Server) accountMoveEnabled() bool { return s.accountMoverFn() != nil }

func (s *Server) toolTaskMoveAccount(id interface{}, args json.RawMessage) *Response {
	move := s.accountMoverFn()
	if move == nil {
		return toolError(id, "task_move_account is not available: the daemon REST API is not enabled")
	}
	var p struct {
		TaskID   string `json:"task_id"`
		Account  string `json:"account"`
		Force    bool   `json:"force"`
		Backend  string `json:"backend"`
		CallerID string `json:"caller_id"`
		Cwd      string `json:"cwd"`
	}
	json.Unmarshal(args, &p) //nolint:errcheck
	p.TaskID, p.Account = strings.TrimSpace(p.TaskID), strings.TrimSpace(p.Account)
	p.Backend = strings.TrimSpace(p.Backend)
	if p.TaskID == "" || (p.Account == "" && p.Backend == "") {
		return toolError(id, "task_id and account are required (account may be omitted when backend is set)")
	}
	callerID := strings.TrimSpace(p.CallerID)
	if callerID == "" && p.Cwd != "" && s.taskDB != nil {
		if caller, err := s.resolveTask("", p.Cwd); err == nil {
			callerID = caller.ID
		}
	}
	if callerID == p.TaskID {
		return toolError(id, "a task cannot move itself (the restart would end this session); ask your coordinator or the user to move it")
	}
	res, err := move(agent.AccountMoveInput{TaskID: p.TaskID, Account: p.Account, Backend: strings.TrimSpace(p.Backend), Force: p.Force})
	if err != nil {
		log.Printf("[mcp] task_move_account task=%s account=%s: %v", p.TaskID, p.Account, err)
		return toolError(id, err.Error())
	}
	log.Printf("[mcp] task_move_account task=%s %s→%s resumed=%t backend=%s→%s", p.TaskID, res.From, res.To, res.Resumed, res.FromBackend, res.ToBackend)
	out, _ := json.Marshal(res)
	return toolResult(id, string(out))
}
