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
		c.API.Enabled = true
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
		{"api off", func(c *Config) { c.API.Enabled = false }, "api.enabled"},
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

// Exact entries stay disjoint; the broader Argus wildcard deliberately overlaps
// structural Hera restrictions, where deny > ask > allow resolves precedence.
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
	// Every external grant is concrete. A future connector write must not
	// accidentally inherit a whole-server or read-name wildcard allow.
	for _, tool := range r.AllowTools {
		if strings.HasPrefix(tool, "mcp__") && !strings.HasPrefix(tool, "mcp__argus__") {
			testutil.Equal(t, strings.ContainsAny(tool, "*?"), false)
		}
	}
}

func TestResidentPrereq_APIOffMessageIsActionable(t *testing.T) {
	enforceResidentPermissions(t)
	c := DefaultConfig()
	c.Resident.Enabled = true
	c.Resident.Name = "Solo"
	c.KB.Enabled = true
	c.KB.MetisVaultPath = t.TempDir()
	c.API.Enabled = false
	err := c.ResidentPrereq()
	if err == nil || !strings.Contains(err.Error(), "api.enabled = true") {
		t.Fatalf("ResidentPrereq() = %v, want actionable api.enabled refusal", err)
	}
}

// TestResidentDefaults_NoWriteToolAllowed pins that no known write/send tool
// from the outward connectors can run unprompted: each is in Ask or Deny and
// never in Allow.
func TestResidentDefaults_NoWriteToolAllowed(t *testing.T) {
	r := DefaultResidentConfig(1)
	in := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	writes := []string{
		"mcp__claude_ai_Gmail__send_message", "mcp__claude_ai_Gmail__reply", "mcp__claude_ai_Gmail__forward",
		"mcp__claude_ai_Google_Calendar__create_event", "mcp__claude_ai_Google_Calendar__update_event",
		"mcp__claude_ai_Google_Calendar__delete_event", "mcp__claude_ai_Google_Calendar__respond_to_event",
		"mcp__claude_ai_Superhuman_Mail__send_draft", "mcp__claude_ai_Superhuman_Mail__create_or_update_event",
		"mcp__claude_ai_Superhuman_Mail__share_thread",
		"mcp__claude_ai_Slack__slack_send_message", "mcp__claude_ai_Slack__slack_schedule_message",
		"mcp__claude_ai_Slack__slack_create_conversation", "mcp__claude_ai_Slack__slack_create_canvas",
		"mcp__claude_ai_Slack__slack_update_canvas", "mcp__claude_ai_Slack__slack_create_list",
		"mcp__claude_ai_Slack__slack_add_list_record", "mcp__claude_ai_Slack__slack_update_list",
		"mcp__claude_ai_Slack__slack_update_list_record",
		"mcp__claude_ai_Linear__save_issue", "mcp__claude_ai_Linear__save_comment", "mcp__claude_ai_Linear__save_document",
		"mcp__claude_ai_Linear__save_project", "mcp__claude_ai_Linear__save_milestone", "mcp__claude_ai_Linear__save_status_update",
		"mcp__claude_ai_Linear__save_customer", "mcp__claude_ai_Linear__save_initiative", "mcp__claude_ai_Linear__delete_comment",
		"mcp__claude_ai_Notion__notion-create-pages", "mcp__claude_ai_Notion__notion-update-page",
		"mcp__claude_ai_Notion__notion-create-comment", "mcp__claude_ai_Notion__notion-create-database",
		"mcp__claude_ai_Notion__notion-update-data-source", "mcp__claude_ai_Notion__notion-move-pages",
		"mcp__claude_ai_Notion__notion-duplicate-page", "mcp__claude_ai_Notion__notion-delete-pages",
		"mcp__claude_ai_Notion__notion-calendar-create-events",
		"mcp__claude_ai_Notion__notion-calendar-update-events",
		"mcp__claude_ai_Notion__notion-calendar-cancel-events",
		"mcp__claude_ai_Notion__notion-update-comment",
		"mcp__claude_ai_Notion__notion-delete-comment",
		"mcp__claude_ai_Notion__notion-delete-databases",
		"mcp__claude_ai_Notion__notion-create-view",
		"mcp__claude_ai_Notion__notion-update-view",
		"mcp__claude_ai_Notion__notion-create-folder",
		"mcp__claude_ai_Notion__notion-update-folder",
		"mcp__claude_ai_Notion__notion-restore-pages",
		"mcp__claude_ai_Notion__notion-restore-version",
		"mcp__claude_ai_Notion__notion-create-suggested-edit",
		"mcp__claude_ai_Notion__notion-resolve-suggested-edit",
		"mcp__claude_ai_Notion__notion-set-comment-reaction",
		"mcp__claude_ai_Notion__notion-set-discussion-resolved",
		"mcp__claude_ai_Notion__notion-create-meeting-note",
		"mcp__claude_ai_Notion__notion-link-meeting-note-to-calendar-event",
		"mcp__claude_ai_Notion__notion-create-attachment",
		"mcp__claude_ai_Notion__notion-create-file-upload",
		"mcp__claude_ai_Notion__notion-spawn-session",
		"mcp__claude_ai_Notion__notion-send-message-to-session",
		"mcp__claude_ai_Notion__notion-stop-session",
		"mcp__claude_ai_Notion__notion-upload-skill",
		"mcp__claude_ai_Notion__notion-update-skill-settings",
		"mcp__claude_ai_Notion__notion-convert-page-to-skill",
		"mcp__claude_ai_Notion__notion-convert-to-meetings-database",
		"mcp__claude_ai_Linear__merge_diff",
		"mcp__claude_ai_Linear__submit_diff_review",
		"mcp__claude_ai_Linear__update_diff",
		"mcp__claude_ai_Linear__resolve_diff_thread",
		"mcp__claude_ai_Linear__retire_issue_label",
		"mcp__claude_ai_Linear__retire_project_label",
		"mcp__claude_ai_Linear__retire_initiative_label",
		"mcp__claude_ai_Linear__restore_issue_label",
		"mcp__claude_ai_Linear__restore_project_label",
		"mcp__claude_ai_Linear__restore_initiative_label",
		"mcp__claude_ai_Linear__mark_notification",
		"mcp__claude_ai_Slack__slack_add_reaction",
		"mcp__claude_ai_Superhuman_Mail__invite_teammate",
		"mcp__claude_ai_Superhuman_Mail__unsubscribe",
		"mcp__claude_ai_Superhuman_Mail__create_shared_thread_comment",
		"mcp__claude_ai_Superhuman_Mail__react_shared_thread_comment",
		"mcp__claude_ai_Superhuman_Mail__unshare_thread",
		"mcp__claude_ai_Superhuman_Mail__undo_send",
	}
	for _, w := range writes {
		t.Run(w, func(t *testing.T) {
			testutil.Equal(t, in(r.AllowTools, w), false)
			testutil.Equal(t, in(r.AskTools, w) || in(r.DenyTools, w), true)
		})
	}
	// Slack send/schedule stay refused outright, never merely prompted.
	testutil.Equal(t, in(r.DenyTools, "mcp__claude_ai_Slack__slack_send_message"), true)
	testutil.Equal(t, in(r.DenyTools, "mcp__claude_ai_Slack__slack_schedule_message"), true)
}
