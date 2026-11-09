package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drn/argus/internal/testutil"
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

func TestResidentPrereq(t *testing.T) {
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
	testutil.DeepEqual(t, r.AskTools, []string{"a"})
	testutil.DeepEqual(t, r.DenyTools, []string{"b"})
}
