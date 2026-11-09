package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentDefaults(t *testing.T) {
	cfg := DefaultConfig()
	r := cfg.Resident
	testutil.Equal(t, r.Enabled, false)
	testutil.Equal(t, r.Name, "")
	testutil.Equal(t, r.MemoryDir, "resident")
	testutil.Equal(t, r.ContextBudget, cfg.Hera.CoordinatorContextBudget)
	testutil.Equal(t, r.Model, "")
	testutil.Equal(t, r.Account, "")
	testutil.Equal(t, len(r.DenyTools) > 0, true)
	testutil.Equal(t, len(r.AskTools) > 0, true)
}

func TestResidentValidName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"simple", "Solo", true},
		{"twelve runes", "abcdefghijkl", true},
		{"thirteen runes", "abcdefghijklm", false},
		{"empty", "", false},
		{"control char", "So\x1blo", false},
		{"newline", "So\nlo", false},
		{"multibyte twelve", "ééééééééééé" + "é", true},
		{"multibyte thirteen", strings.Repeat("é", 13), false},
		{"spaces ok", "Dot Two", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidResidentName(tt.in)
			testutil.Equal(t, err == nil, tt.ok)
		})
	}
}

func TestResidentPermissionsEnforcedShipsTrue(t *testing.T) {
	testutil.Equal(t, ResidentPermissionsEnforced, true)
}

func TestResidentPrereq_RefusesWhilePermissionsUnenforced(t *testing.T) {
	c := DefaultConfig()
	c.Resident.Enabled = true
	c.Resident.Name = "Solo"
	c.KB.Enabled = true
	c.KB.MetisVaultPath = t.TempDir()
	old := ResidentPermissionsEnforced
	ResidentPermissionsEnforced = false // the gate itself, now shipped true
	t.Cleanup(func() { ResidentPermissionsEnforced = old })
	if err := c.ResidentPrereq(); err == nil || !strings.Contains(err.Error(), "not enforced") {
		t.Fatalf("ResidentPrereq() = %v, want not-enforced refusal", err)
	}
}

func TestResidentPrereq(t *testing.T) {
	enforceResidentPermissions(t)
	vault := t.TempDir()
	good := func() Config {
		c := DefaultConfig()
		c.Resident.Enabled = true
		c.Resident.Name = "Solo"
		c.KB.Enabled = true
		c.KB.MetisVaultPath = vault
		return c
	}
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"ok", func(*Config) {}, ""},
		{"disabled", func(c *Config) { c.Resident.Enabled = false }, "not enabled"},
		{"no name", func(c *Config) { c.Resident.Name = "" }, "valid name"},
		{"bad name", func(c *Config) { c.Resident.Name = "x\x00" }, "valid name"},
		{"kb off", func(c *Config) { c.KB.Enabled = false }, "kb.enabled"},
		{"vault unset", func(c *Config) { c.KB.MetisVaultPath = "" }, "metis_vault_path"},
		{"vault missing", func(c *Config) { c.KB.MetisVaultPath = filepath.Join(vault, "nope") }, "metis_vault_path"},
		{"vault is file", func(c *Config) {
			f := filepath.Join(vault, "f")
			_ = os.WriteFile(f, nil, 0o644)
			c.KB.MetisVaultPath = f
		}, "metis_vault_path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := good()
			tt.mutate(&c)
			err := c.ResidentPrereq()
			if tt.wantErr == "" {
				testutil.NoError(t, err)
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ResidentPrereq() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResidentTomlOverlay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	testutil.NoError(t, os.WriteFile(p, []byte(`
[resident]
enabled = true
name = "Dot"
context_budget = 123000
memory_dir = "assistant"
model = "opus"
account = "work"
allow_tools = ["c"]
ask_tools = ["a"]
deny_tools = ["b"]
`), 0o644))
	got := NewFileLoader(p).Apply(DefaultConfig())
	r := got.Resident
	testutil.Equal(t, r.Enabled, true)
	testutil.Equal(t, r.Name, "Dot")
	testutil.Equal(t, r.ContextBudget, 123000)
	testutil.Equal(t, r.MemoryDir, "assistant")
	testutil.Equal(t, r.Model, "opus")
	testutil.Equal(t, r.Account, "work")
	testutil.DeepEqual(t, r.AllowTools, []string{"c"})
	testutil.DeepEqual(t, r.AskTools, []string{"a"})
	testutil.DeepEqual(t, r.DenyTools, []string{"b"})
}

func enforceResidentPermissions(t *testing.T) {
	t.Helper()
	old := ResidentPermissionsEnforced
	ResidentPermissionsEnforced = true
	t.Cleanup(func() { ResidentPermissionsEnforced = old })
}

// The tiers must be disjoint: an allow entry that also appears in ask or deny
// would make the intended policy ambiguous, and Slack send must never be allowed.
func TestDefaultResidentConfig_TiersAreDisjoint(t *testing.T) {
	r := DefaultResidentConfig(1)
	seen := map[string]string{}
	for tier, list := range map[string][]string{"allow": r.AllowTools, "ask": r.AskTools, "deny": r.DenyTools} {
		for _, tool := range list {
			if prev, dup := seen[tool]; dup {
				t.Errorf("%s is in both %s and %s", tool, prev, tier)
			}
			seen[tool] = tier
		}
	}
	for _, send := range []string{"mcp__claude_ai_Slack__slack_send_message", "mcp__claude_ai_Slack__slack_schedule_message"} {
		testutil.Equal(t, seen[send], "deny")
	}
	testutil.Equal(t, seen["mcp__claude_ai_Slack__slack_send_message_draft"], "allow")
}

// Property: outward-acting tools never sit in the unprompted allow tier, and
// the tools that must prompt or be refused are in the right tier.
func TestDefaultResidentConfig_OutwardActionsNeverAllowed(t *testing.T) {
	r := DefaultResidentConfig(1)
	has := func(list []string, want string) bool {
		for _, x := range list {
			if x == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"mcp__claude_ai_Google_Calendar__create_event", "mcp__claude_ai_Google_Calendar__update_event",
		"mcp__claude_ai_Google_Calendar__delete_event", "mcp__claude_ai_Google_Calendar__respond_to_event",
		"mcp__claude_ai_Gmail__send_message", "mcp__claude_ai_Gmail__reply", "mcp__claude_ai_Gmail__forward",
		"mcp__claude_ai_Superhuman_Mail__send_draft",
		"mcp__claude_ai_Slack__slack_create_canvas", "mcp__claude_ai_Slack__slack_create_list",
	} {
		if !has(r.AskTools, want) {
			t.Errorf("AskTools missing %s", want)
		}
	}
	for _, want := range []string{"mcp__claude_ai_Slack__slack_send_message", "mcp__claude_ai_Slack__slack_schedule_message"} {
		if !has(r.DenyTools, want) {
			t.Errorf("DenyTools missing %s", want)
		}
	}
	const draft = "mcp__claude_ai_Slack__slack_send_message_draft"
	outward := []string{"send", "create_", "update", "delete", "respond", "reply", "forward", "schedule", "share"}
	for _, tool := range r.AllowTools {
		if tool == draft {
			continue
		}
		name := tool[strings.LastIndex(tool, "__")+2:]
		for _, verb := range outward {
			if strings.Contains(name, verb) {
				t.Errorf("allow tier holds outward-acting tool %s (matches %q)", tool, verb)
			}
		}
	}
}
