package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const exchangeTranscript = `{"type":"user","message":{"role":"user","content":"is it CPU or Argus?"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Mostly host CPU load."}]}}
`

func claudeCfg() config.Config {
	return config.Config{
		Defaults: config.Defaults{Backend: "claude"},
		Backends: map[string]config.Backend{"claude": {Command: "claude"}},
		Projects: map[string]config.Project{},
	}
}

func writeTranscript(t *testing.T, home, worktree, sid, body string) {
	t.Helper()
	proj, err := claudesession.ProjectDirIn(filepath.Join(home, ".claude"), worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, os.MkdirAll(proj, 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(body), 0o600))
}

func TestRecycleExchangeSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.NoError(t, os.MkdirAll(wt, 0o755))
	writeTranscript(t, home, wt, "sid-1", exchangeTranscript)

	task := &model.Task{ID: "t1", Worktree: wt, SessionID: "sid-1", Backend: "claude"}

	t.Run("claude transcript extracted", func(t *testing.T) {
		got := recycleExchangeSection(task, claudeCfg())
		testutil.Contains(t, got, "Last exchange with the user before the reset")
		testutil.Contains(t, got, "is it CPU or Argus?")
		testutil.Contains(t, got, "Mostly host CPU load.")
	})
	t.Run("missing transcript omits", func(t *testing.T) {
		other := *task
		other.SessionID = "nope"
		testutil.Equal(t, recycleExchangeSection(&other, claudeCfg()), "")
	})
	t.Run("non-claude backend omits", func(t *testing.T) {
		cfg := claudeCfg()
		cfg.Backends["claude"] = config.Backend{Command: "codex"}
		testutil.Equal(t, recycleExchangeSection(task, cfg), "")
	})
	t.Run("empty session or traversal omits", func(t *testing.T) {
		bad := *task
		bad.SessionID = ""
		testutil.Equal(t, recycleExchangeSection(&bad, claudeCfg()), "")
		bad.SessionID = "../x"
		testutil.Equal(t, recycleExchangeSection(&bad, claudeCfg()), "")
		testutil.Equal(t, recycleExchangeSection(nil, claudeCfg()), "")
	})
}

func TestBuildTaskRecycleSeedPrompt_LastExchange(t *testing.T) {
	got := buildTaskRecycleSeedPrompt("ZZMISSION", "note", "\n## Last exchange with the user before the reset\nQ\n")
	testutil.Contains(t, got, "Last exchange with the user before the reset")
	testutil.Contains(t, got, "FIRST user-visible")
	if strings.Index(got, "Last exchange") > strings.Index(got, "ZZMISSION") {
		t.Fatal("last exchange must precede the original prompt")
	}
}

func TestRecycleExchangeSection_Resident(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resident := &model.Task{ID: "r1", Worktree: db.ResidentWorkDir(), SessionID: "sid-r", Backend: "claude"}
	testutil.NoError(t, os.MkdirAll(resident.Worktree, 0o755))
	writeTranscript(t, home, resident.Worktree, "sid-r", exchangeTranscript)

	got := recycleExchangeSection(resident, claudeCfg())
	testutil.Contains(t, got, "Last exchange with the user before the reset")
	testutil.Contains(t, got, "Mostly host CPU load.")
}
