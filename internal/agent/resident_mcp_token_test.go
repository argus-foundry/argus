package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// residentMCPConfigToken parses the resident's private MCP config and returns
// the argus server's URL and token header.
func residentMCPConfigToken(t *testing.T, path string) (url, token string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	testutil.NoError(t, err)
	var cfg struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	testutil.NoError(t, json.Unmarshal(raw, &cfg))
	srv := cfg.MCPServers["argus"]
	return srv.URL, srv.Headers[ResidentMCPTokenHeader]
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Lstat(path)
	testutil.NoError(t, err)
	testutil.Equal(t, fi.Mode().Perm(), want)
}

func TestBuildCmd_ResidentMCPTokenFileOnlyAndRotates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.MCPPort = 7742
	res := &model.Task{ID: "res-9", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	launchDir := filepath.Join(home, ".argus", "resident-launch")
	configPath := filepath.Join(launchDir, "mcp-config.json")
	hashPath, err := ResidentMCPTokenHashPath()
	testutil.NoError(t, err)
	testutil.Equal(t, hashPath, filepath.Join(evalSymlinksOrKeep(filepath.Join(home, ".argus")), "resident-launch", "mcp-token.sha256"))

	launch := func() string {
		t.Helper()
		cmd, cleanup, err := BuildCmd(res, cfg, false)
		testutil.NoError(t, err)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		argv := strings.Join(cmd.Args, " ")
		url, token := residentMCPConfigToken(t, configPath)
		testutil.Equal(t, url, "http://localhost:7742/mcp?caller=res-9")
		testutil.True(t, regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(token))
		// Neither argv nor env carries the token or the inline config.
		testutil.Contains(t, argv, "--mcp-config "+shellQuote(configPath))
		for _, leak := range []string{token, "mcpServers", "caller="} {
			if strings.Contains(argv, leak) {
				t.Fatalf("argv leaks %q: %s", leak, argv)
			}
		}
		for _, kv := range cmd.Env {
			if strings.Contains(kv, token) {
				t.Fatalf("env leaks the token: %s", strings.SplitN(kv, "=", 2)[0])
			}
		}
		// Only the hash is persisted beside the config, both private.
		hash, err := os.ReadFile(hashPath)
		testutil.NoError(t, err)
		testutil.Equal(t, strings.TrimSpace(string(hash)), HashResidentMCPToken(token))
		if strings.Contains(string(hash), token) {
			t.Fatal("hash file holds the raw token")
		}
		assertMode(t, launchDir, 0o700)
		assertMode(t, configPath, 0o600)
		assertMode(t, hashPath, 0o600)
		return token
	}

	first := launch()
	testutil.True(t, VerifyResidentMCPToken(hashPath, first))
	second := launch() // resume / kick / recycle all rebuild through BuildCmd
	testutil.True(t, first != second)
	testutil.False(t, VerifyResidentMCPToken(hashPath, first))
	testutil.True(t, VerifyResidentMCPToken(hashPath, second))
}

func TestBuildCmd_ResidentMCPConfigReplacesPlantedSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	launchDir := filepath.Join(home, ".argus", "resident-launch")
	testutil.NoError(t, os.MkdirAll(launchDir, 0o700))
	victim := filepath.Join(t.TempDir(), "victim")
	testutil.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o600))
	for _, name := range []string{"mcp-config.json", "mcp-token.sha256"} {
		testutil.NoError(t, os.Symlink(victim, filepath.Join(launchDir, name)))
	}
	cfg := residentCfg()
	cfg.MCPPort = 7742
	_, cleanup, err := BuildCmd(&model.Task{ID: "res-9", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	got, err := os.ReadFile(victim)
	testutil.NoError(t, err)
	testutil.Equal(t, string(got), "untouched")
	for _, name := range []string{"mcp-config.json", "mcp-token.sha256"} {
		fi, err := os.Lstat(filepath.Join(launchDir, name))
		testutil.NoError(t, err)
		testutil.True(t, fi.Mode().IsRegular())
	}
}

func TestBuildCmd_ResidentWithoutMCPRetiresToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.MCPPort = 7742
	res := &model.Task{ID: "res-9", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	_, cleanup, err := BuildCmd(res, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	hashPath, err := ResidentMCPTokenHashPath()
	testutil.NoError(t, err)
	_, err = os.Stat(hashPath)
	testutil.NoError(t, err)
	cfg.MCPPort = 0
	_, cleanup, err = BuildCmd(res, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	_, err = os.Stat(hashPath)
	testutil.True(t, os.IsNotExist(err))
}

func TestBuildCmd_PlainTaskKeepsInlineUnboundMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := testConfig()
	cfg.MCPPort = 7742
	cmd, cleanup, err := BuildCmd(&model.Task{ID: "p1", Name: "plain", Worktree: t.TempDir()}, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	argv := strings.Join(cmd.Args, " ")
	testutil.Contains(t, argv, `"url":"http://localhost:7742/mcp"`)
	for _, absent := range []string{"caller=", "headers", ResidentMCPTokenHeader} {
		if strings.Contains(argv, absent) {
			t.Fatalf("plain task argv carries %q: %s", absent, argv)
		}
	}
	_, err = os.Stat(filepath.Join(home, ".argus", "resident-launch", "mcp-token.sha256"))
	testutil.True(t, os.IsNotExist(err))
}

func TestVerifyResidentMCPToken(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "hash")
	testutil.NoError(t, os.WriteFile(good, []byte(HashResidentMCPToken("secret")+"\n"), 0o600))
	link := filepath.Join(dir, "link")
	testutil.NoError(t, os.Symlink(good, link))
	short := filepath.Join(dir, "short")
	testutil.NoError(t, os.WriteFile(short, []byte("abc"), 0o600))
	for _, tc := range []struct {
		name, path, token string
		want              bool
	}{
		{"match", good, "secret", true},
		{"wrong", good, "other", false},
		{"empty token", good, "", false},
		{"missing file", filepath.Join(dir, "none"), "secret", false},
		{"symlinked hash", link, "secret", false},
		{"malformed hash", short, "secret", false},
		{"empty path", "", "secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, VerifyResidentMCPToken(tc.path, tc.token), tc.want)
		})
	}
}

// sandboxProfileOf returns the SBPL profile text BuildCmd wrapped cmd with.
func sandboxProfileOf(t *testing.T, cfg config.Config, task *model.Task) string {
	t.Helper()
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	m := regexp.MustCompile(`-f '?([^' ]+\.sb)'?`).FindStringSubmatch(strings.Join(cmd.Args, " "))
	if m == nil {
		t.Fatalf("command not sandbox-wrapped: %v", cmd.Args)
	}
	raw, err := os.ReadFile(m[1])
	testutil.NoError(t, err)
	return string(raw)
}

func TestBuildCmd_ResidentLaunchDirDeniedToOtherSandboxedTasks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ForceSandboxAvailableForTest(t, true)
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Sandbox.Enabled = true
	launch := evalSymlinksOrKeep(home) + "/.argus/resident-launch"
	deny := `(deny file-read* (subpath "` + launch + `"))`

	plain := sandboxProfileOf(t, cfg, &model.Task{ID: "p1", Name: "plain", Worktree: t.TempDir()})
	testutil.Contains(t, plain, deny)
	// The resident reads its own launch files (system prompt, MCP config).
	resident := sandboxProfileOf(t, cfg, &model.Task{ID: "res-9", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()})
	if strings.Contains(resident, "resident-launch") {
		t.Fatalf("resident profile denies its own launch dir:\n%s", resident)
	}
}
