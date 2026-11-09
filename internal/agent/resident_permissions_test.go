package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func settingsArg(t *testing.T, args []string) (map[string]any, bool) {
	t.Helper()
	_, rest, ok := strings.Cut(strings.Join(args, " "), "--settings '")
	if !ok {
		return nil, false
	}
	raw, _, _ := strings.Cut(rest, "'")
	var out map[string]any
	testutil.NoError(t, json.Unmarshal([]byte(raw), &out))
	return out, true
}

func residentCfg() config.Config {
	cfg := testConfig()
	cfg.Resident = config.DefaultResidentConfig(100000)
	return cfg
}

func TestBuildCmd_ResidentGetsAskAndDenyRules(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, residentCfg(), false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	st, ok := settingsArg(t, cmd.Args)
	if !ok {
		t.Fatalf("resident command has no --settings: %v", cmd.Args)
	}
	perms, _ := st["permissions"].(map[string]any)
	toStrings := func(v any) []string {
		var out []string
		for _, x := range v.([]any) {
			out = append(out, x.(string))
		}
		return out
	}
	ask, deny := toStrings(perms["ask"]), toStrings(perms["deny"])
	testutil.Contains(t, strings.Join(ask, ","), "mcp__claude_ai_Google_Calendar__create_event")
	testutil.Contains(t, strings.Join(deny, ","), "mcp__claude_ai_Slack__slack_send_message")
	for _, d := range deny {
		if strings.HasSuffix(d, "slack_send_message_draft") {
			t.Fatalf("draft tool must not be denied: %v", deny)
		}
	}
}

func TestBuildCmd_ResidentRulesHonorConfigLists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Resident.AskTools = []string{"mcp__x__ask"}
	cfg.Resident.DenyTools = []string{"mcp__x__deny"}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	st, _ := settingsArg(t, cmd.Args)
	perms := st["permissions"].(map[string]any)
	testutil.DeepEqual(t, perms["ask"], any([]any{"mcp__x__ask"}))
	testutil.DeepEqual(t, perms["deny"], any([]any{"mcp__x__deny"}))
}

func TestBuildCmd_NonResidentGetsNoResidentRules(t *testing.T) {
	cfg := residentCfg()
	task := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if _, ok := settingsArg(t, cmd.Args); ok {
		t.Fatalf("plain task must not receive resident permission rules: %v", cmd.Args)
	}
}

func TestBuildCmd_ResidentEmptyListsInjectNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Resident.AskTools, cfg.Resident.DenyTools = nil, nil
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if _, ok := settingsArg(t, cmd.Args); ok {
		t.Fatalf("empty lists should inject no --settings: %v", cmd.Args)
	}
}
