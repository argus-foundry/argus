package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/lastexchange"
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
	wd := db.ResidentWorkDir()
	testutil.NoError(t, os.MkdirAll(wd, 0o755))

	t.Run("default account", func(t *testing.T) {
		resident := &model.Task{ID: "r1", Kind: model.KindResident, Worktree: wd, SessionID: "sid-r", Backend: "claude"}
		writeTranscript(t, home, wd, "sid-r", exchangeTranscript)

		got := recycleExchangeSection(resident, claudeCfg())
		testutil.Contains(t, got, "Last exchange with the user before the reset")
		testutil.Contains(t, got, "Mostly host CPU load.")
	})

	t.Run("recycle before any human turn quotes no Argus text", func(t *testing.T) {
		// The session's only user turns are Argus's own: the orientation it
		// launched with, a previous recycle seed, and a budget nudge.
		cfg := claudeCfg()
		cfg.Resident.Name = "Sol"
		var body strings.Builder
		for _, text := range []string{
			buildResidentOrientation(cfg),
			buildResidentRecycleSeedPrompt("orientation", "old handoff", "resident", ""),
			lastexchange.SystemTurnPrefix + "Context budget reached (1/1 tokens).",
		} {
			line, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
			testutil.NoError(t, err)
			body.Write(line)
			body.WriteString("\n" + `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ARGUS-ONLY-REPLY"}]}}` + "\n")
		}
		resident := &model.Task{ID: "r0", Kind: model.KindResident, Worktree: wd, SessionID: "sid-0", Backend: "claude"}
		writeTranscript(t, home, wd, "sid-0", body.String())
		got := recycleExchangeSection(resident, cfg)
		testutil.Equal(t, got, "")
	})

	t.Run("named account reads its own config dir", func(t *testing.T) {
		acctDir := filepath.Join(home, "acct-work")
		cfg := claudeCfg()
		cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: acctDir}}
		const accountExchange = `{"type":"user","message":{"role":"user","content":"account question"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ACCOUNT-DIR-REPLY"}]}}
`
		const decoyExchange = `{"type":"user","message":{"role":"user","content":"decoy question"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"DEFAULT-DIR-DECOY"}]}}
`
		proj, err := claudesession.ProjectDirIn(acctDir, wd)
		testutil.NoError(t, err)
		testutil.NoError(t, os.MkdirAll(proj, 0o755))
		testutil.NoError(t, os.WriteFile(filepath.Join(proj, "sid-w.jsonl"), []byte(accountExchange), 0o600))
		writeTranscript(t, home, wd, "sid-w", decoyExchange) // same session ID under the default dir

		resident := &model.Task{ID: "r2", Kind: model.KindResident, Worktree: wd, SessionID: "sid-w", Backend: "claude", Account: "work"}
		got := recycleExchangeSection(resident, cfg)
		testutil.Contains(t, got, "ACCOUNT-DIR-REPLY")
		testutil.False(t, strings.Contains(got, "DEFAULT-DIR-DECOY"))

		// Control: without the account the decoy is what resolves.
		resident.Account = ""
		testutil.Contains(t, recycleExchangeSection(resident, cfg), "DEFAULT-DIR-DECOY")
	})
}
