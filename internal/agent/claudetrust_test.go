package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCreateAndStart_ClaudeTrust(t *testing.T) {
	for _, tc := range []struct {
		name, account, command, toml, initial string
		trust                                 bool
		locked                                bool
		skillsErr                             bool
	}{
		{name: "plain", command: "claude", trust: true},
		{name: "named account", account: "work", command: "claude", trust: true},
		{name: "aliased backend", command: "/usr/local/bin/claude --verbose", trust: true},
		{name: "disabled", command: "claude", toml: "[agents]\ntrust_worktrees = false\n"},
		{name: "other family", command: "codex"},
		{name: "fail open", command: "claude", initial: "{bad json"},
		{name: "lock busy fails open", command: "claude", locked: true},
		{name: "skills unavailable", command: "claude", trust: true, skillsErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initGitRepo(t) // redirects HOME before any creation
			skillsRoot := filepath.Join(os.Getenv("HOME"), ".argus", "skills")
			oldSkills := ensureSessionSkillsFn
			ensureSessionSkillsFn = func() (string, error) {
				if tc.skillsErr {
					return "", errors.New("materialize failed")
				}
				return skillsRoot, os.MkdirAll(skillsRoot, 0o755)
			}
			t.Cleanup(func() { ensureSessionSkillsFn = oldSkills })
			dir := t.TempDir()
			accountDir := filepath.Join(os.Getenv("HOME"), "claude-work")
			contents := tc.toml + "\n[accounts.work]\nclaude_config_dir = " + strconv.Quote(accountDir) + "\n"
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(contents), 0o600))
			d, err := db.Open(filepath.Join(dir, "data.sql"))
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			testutil.NoError(t, d.SetBackend("aliased", config.Backend{Command: tc.command}))
			testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "HEAD"}))
			path := filepath.Join(os.Getenv("HOME"), ".claude.json")
			if tc.account != "" {
				path = filepath.Join(accountDir, ".claude.json")
			}
			if tc.initial != "" {
				testutil.NoError(t, os.WriteFile(path, []byte(tc.initial), 0o600))
			}
			if tc.locked {
				testutil.NoError(t, os.Mkdir(path+".lock", 0o700))
			}
			wt := ""
			runner := &fakeRunner{sessionPID: 42}
			task, _, err := CreateAndStart(d, runner, CreateInput{
				Name: "trust", Project: "proj", Backend: "aliased", Account: tc.account,
				OnWorktreeCreated: func(p string) error { wt = p; return nil },
				BeforeStart: func() {
					raw, err := os.ReadFile(path)
					if tc.trust {
						testutil.NoError(t, err)
						var data struct {
							Projects map[string]struct {
								Trusted bool `json:"hasTrustDialogAccepted"`
							} `json:"projects"`
						}
						testutil.NoError(t, json.Unmarshal(raw, &data))
						canonical, err := filepath.EvalSymlinks(wt)
						testutil.NoError(t, err)
						testutil.True(t, data.Projects[canonical].Trusted)
						canonicalSkills, err := filepath.EvalSymlinks(skillsRoot)
						if tc.skillsErr {
							testutil.True(t, os.IsNotExist(err))
						} else {
							testutil.NoError(t, err)
							testutil.True(t, data.Projects[canonicalSkills].Trusted)
						}
					} else if tc.initial != "" {
						testutil.NoError(t, err)
						testutil.Equal(t, string(raw), tc.initial)
					} else {
						testutil.True(t, os.IsNotExist(err))
					}
				},
			})
			testutil.NoError(t, err)
			testutil.Equal(t, task.Status, model.StatusInProgress)
			testutil.Equal(t, runner.startCalls, 1)
			if tc.locked {
				info, err := os.Stat(path + ".lock")
				testutil.NoError(t, err)
				testutil.True(t, info.IsDir())
			}
			if tc.account != "" {
				_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".claude.json"))
				testutil.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestTrustClaudeWorktree_EnvMapping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "override")
	t.Setenv("ARGUS_TEST_TRUST_DIR", dir)
	cfg := config.DefaultConfig()
	cfg.Backends["claude"] = config.Backend{Command: "claude", EnvVars: map[string]string{"CLAUDE_CONFIG_DIR": "env://ARGUS_TEST_TRUST_DIR"}}
	testutil.NoError(t, trustClaudeWorktree(&model.Task{Backend: "claude", Worktree: t.TempDir()}, cfg))
	_, err := os.Stat(filepath.Join(dir, ".claude.json"))
	testutil.NoError(t, err)
	_, err = os.Stat(filepath.Join(os.Getenv("HOME"), ".claude.json"))
	testutil.True(t, os.IsNotExist(err))
}

func TestBuildCmd_ResidentTrustsLaunchDirs(t *testing.T) {
	for _, tc := range []struct {
		name, initial string
		disabled      bool
		wantTrust     bool
	}{
		{name: "named account", wantTrust: true},
		{name: "disabled", disabled: true},
		{name: "malformed config fails open", initial: "{bad json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, _ := residentFixture(t)
			home := os.Getenv("HOME")
			acct := filepath.Join(home, "claude-work")
			testutil.NoError(t, os.MkdirAll(filepath.Join(acct, "skills"), 0o755))
			skillsRoot := filepath.Join(home, ".argus", "skills")
			oldSkills := ensureSessionSkillsFn
			ensureSessionSkillsFn = func() (string, error) { return skillsRoot, os.MkdirAll(skillsRoot, 0o755) }
			t.Cleanup(func() { ensureSessionSkillsFn = oldSkills })
			cfg := residentCfg()
			cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: acct}}
			cfg.Agents.TrustWorktrees = !tc.disabled
			task.Account = "work"
			path := filepath.Join(acct, ".claude.json")
			if tc.initial != "" {
				testutil.NoError(t, os.WriteFile(path, []byte(tc.initial), 0o600))
			}

			buildResident(t, task, cfg)

			raw, err := os.ReadFile(path)
			switch {
			case tc.initial != "":
				testutil.NoError(t, err)
				testutil.Equal(t, string(raw), tc.initial)
			case !tc.wantTrust:
				testutil.True(t, os.IsNotExist(err))
			default:
				testutil.NoError(t, err)
				var data struct {
					Projects map[string]struct {
						Trusted bool `json:"hasTrustDialogAccepted"`
					} `json:"projects"`
				}
				testutil.NoError(t, json.Unmarshal(raw, &data))
				userSkills := ensureResidentUserSkillsDir(task, cfg)
				testutil.True(t, userSkills != "")
				for _, dir := range []string{db.ResidentWorkDir(), skillsRoot, userSkills} {
					canonical, err := filepath.EvalSymlinks(dir)
					testutil.NoError(t, err)
					testutil.True(t, data.Projects[canonical].Trusted)
				}
				_, err := os.Stat(filepath.Join(home, ".claude.json"))
				testutil.True(t, os.IsNotExist(err))
			}
		})
	}
}
