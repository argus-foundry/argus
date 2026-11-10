package mcp

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLogs routes slog (and the standard log package) and uxlog into
// buffers for the rest of the test, restoring both afterwards.
func captureLogs(t *testing.T) (slogBuf *syncBuffer, uxlogPath string) {
	t.Helper()
	slogBuf = &syncBuffer{}
	prevSlog, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(slogBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(prevSlog)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	uxlogPath = filepath.Join(t.TempDir(), "ux.log")
	uxlog.Close()
	testutil.NoError(t, uxlog.Init(uxlogPath))
	t.Cleanup(uxlog.Close)
	return slogBuf, uxlogPath
}

// The resident's token never reaches a log line: not at launch, not on a
// resume, and not when a request presents it wrongly.
func TestResidentToken_NeverLogged(t *testing.T) {
	s, d, res, o := residentHeraFixture(t) // HOME is a temp dir from here on
	worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
	slogBuf, uxlogPath := captureLogs(t)
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := config.Config{
		Defaults: config.Defaults{Backend: "claude"},
		Backends: map[string]config.Backend{"claude": {Command: "claude --dangerously-skip-permissions"}},
		MCPPort:  7742,
		Resident: config.DefaultResidentConfig(100000),
	}
	hashPath, err := agent.ResidentMCPTokenHashPath()
	testutil.NoError(t, err)
	s.residentTokenHashPath = agent.ResidentMCPTokenHashPath
	configPath := filepath.Join(filepath.Dir(hashPath), "mcp-config.json")

	var secrets []string
	launch := func(resume bool) string {
		t.Helper()
		if resume {
			res.SessionID = "11111111-2222-3333-4444-555555555555"
		}
		_, cleanup, err := agent.BuildCmd(res, cfg, resume)
		testutil.NoError(t, err)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		raw, err := os.ReadFile(configPath)
		testutil.NoError(t, err)
		var mc struct {
			MCPServers map[string]struct {
				Headers map[string]string `json:"headers"`
			} `json:"mcpServers"`
		}
		testutil.NoError(t, json.Unmarshal(raw, &mc))
		token := mc.MCPServers["argus"].Headers[agent.ResidentMCPTokenHeader]
		testutil.True(t, len(token) == 64)
		secrets = append(secrets, token, string(raw))
		return token
	}
	first := launch(false)
	token := launch(true)
	path := "/mcp?caller=" + res.ID

	// Wrong, superseded and misplaced tokens are all refused and logged.
	for _, tc := range []struct{ path, token string }{
		{path, "wrong-" + token},
		{path, first},
		{path, token + "x"},
		{"/mcp?caller=" + worker.ID, token},
	} {
		tools := listedTools(t, postRaw(t, s, tc.path, tc.token, "tools/list", map[string]any{}))
		testutil.False(t, tools["hera_delete_worker"])
		cr := callResult(t, postRaw(t, s, tc.path, tc.token, "tools/call", ToolCallParams{Name: "hera_delete_worker", Arguments: json.RawMessage(`{"role":"worker","force":true}`)}))
		testutil.True(t, cr.IsError)
	}
	// The verified resident's own calls log too.
	testutil.True(t, listedTools(t, postRaw(t, s, path, token, "tools/list", map[string]any{}))["hera_delete_worker"])
	cr := callResult(t, postRaw(t, s, path, token, "tools/call", ToolCallParams{Name: "hera_delete_worker", Arguments: json.RawMessage(`{"role":"nobody"}`)}))
	testutil.True(t, cr.IsError)

	ux, err := os.ReadFile(uxlogPath)
	testutil.NoError(t, err)
	// The capture works: both sinks saw the refusals and the rotation.
	testutil.Contains(t, slogBuf.String(), "unverified caller refused")
	testutil.Contains(t, string(ux), "unverified caller refused")
	testutil.Contains(t, string(ux), "MCP identity rotated")
	for i, secret := range secrets {
		for sink, text := range map[string]string{"slog": slogBuf.String(), "uxlog": string(ux)} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaked secret #%d (token or MCP config contents)", sink, i)
			}
		}
	}
}
