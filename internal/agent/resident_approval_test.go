package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentApprovalLaunchRotation(t *testing.T) {
	task, _ := residentFixture(t)
	task.SessionID = "session"
	cmd := buildResident(t, task, residentCfg())
	launch, err := ReadResidentApprovalLaunch()
	testutil.NoError(t, err)
	testutil.Equal(t, launch.TaskID, task.ID)
	testutil.Equal(t, launch.Session, task.SessionID)
	testutil.Equal(t, launch.Cwd, task.Worktree)
	testutil.True(t, launch.Matches(launch.Token, launch.Generation))
	testutil.False(t, launch.Matches("wrong", launch.Generation))
	testutil.False(t, launch.Matches(launch.Token, "stale"))
	line := strings.Join(cmd.Args, " ")
	testutil.Contains(t, line, "resident-permission-hook "+launch.Generation)
	testutil.False(t, strings.Contains(line, launch.Token))
	testutil.False(t, strings.Contains(strings.Join(cmd.Env, " "), launch.Token))
	st, err := os.Stat(ResidentApprovalLaunchPath())
	testutil.NoError(t, err)
	testutil.Equal(t, st.Mode().Perm(), os.FileMode(0o600))
	settings, ok := settingsArg(t, cmd.Args)
	testutil.True(t, ok)
	matcher := settings["hooks"].(map[string]any)["PermissionRequest"].([]any)[0].(map[string]any)["matcher"]
	testutil.Equal(t, matcher, any("Bash|mcp__.*"))
	h := settings["hooks"].(map[string]any)["PermissionRequest"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	testutil.Equal(t, h["onFailure"], any("block"))
	_ = buildResident(t, task, residentCfg())
	next, err := ReadResidentApprovalLaunch()
	testutil.NoError(t, err)
	testutil.False(t, next.Matches(launch.Token, launch.Generation))
	for _, raw := range []string{"", `{}`, `{"Token":"x","Generation":"bad"}`} {
		testutil.NoError(t, os.WriteFile(ResidentApprovalLaunchPath(), []byte(raw), 0o600))
		_, err := ReadResidentApprovalLaunch()
		testutil.Error(t, err)
	}
}
