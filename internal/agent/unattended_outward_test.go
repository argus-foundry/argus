package agent

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// disallowedGroups returns the tool lists of every --disallowedTools flag in a
// shell command line, after POSIX quote removal (what claude's argv sees).
func disallowedGroups(t *testing.T, line string) [][]string {
	t.Helper()
	words, err := splitShellWords(line)
	testutil.NoError(t, err)
	// The sandbox wrap runs the agent command as `sh -c '<command>'`.
	for i := 0; i+2 < len(words); i++ {
		if words[i].value == "sh" && words[i+1].value == "-c" {
			return disallowedGroups(t, words[i+2].value)
		}
	}
	var groups [][]string
	for i := 0; i < len(words); i++ {
		if words[i].value != "--disallowedTools" {
			continue
		}
		var g []string
		for i+1 < len(words) && !strings.HasPrefix(words[i+1].value, "-") {
			i++
			g = append(g, words[i].value)
		}
		groups = append(groups, g)
	}
	return groups
}

func unattendedOutwardLine(t *testing.T, cfg config.Config, worker bool) string {
	t.Helper()
	task := &model.Task{ID: "w", Backend: "claude", Worktree: t.TempDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false, func(string) (bool, error) { return worker, nil })
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return cmd.Args[len(cmd.Args)-1]
}

func unattendedOutwardCfg(t *testing.T) config.Config {
	t.Helper()
	sandboxAvailabilityForSignalTest(t, true)
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Sandbox.Enabled = true
	return cfg
}

// Inventory: every resident ask- and deny-tier rule is denied to an
// unattended worker, and no allow-tier rule (drafts, connector reads) is.
func TestUnattendedOutwardDeny_Inventory(t *testing.T) {
	cfg := config.DefaultConfig()
	deny := ResidentWorkerDenyRules(cfg)
	testutil.True(t, len(cfg.Resident.AskTools) > 0 && len(cfg.Resident.DenyTools) > 0)
	for _, tier := range []struct {
		name  string
		rules []string
	}{{"ask", cfg.Resident.AskTools}, {"deny", cfg.Resident.DenyTools}} {
		for _, r := range tier.rules {
			t.Run(tier.name+"/"+r, func(t *testing.T) { testutil.True(t, slices.Contains(deny, r)) })
		}
	}
	for _, r := range cfg.Resident.AllowTools {
		t.Run("allow/"+r, func(t *testing.T) { testutil.False(t, slices.Contains(deny, r)) })
	}
}

// Derived, not copied: a config.toml / DB override of the resident's tiers is
// what the worker is denied.
func TestUnattendedOutwardDeny_HonorsTierOverrides(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Resident.AskTools = []string{"mcp__custom__write", "Bash(git push:*)"}
	cfg.Resident.DenyTools = []string{"mcp__custom__*"}
	testutil.DeepEqual(t, ResidentWorkerDenyRules(cfg), []string{"mcp__custom__write", "Bash(git push:*)", "mcp__custom__*"})
}

func TestUnattendedOutwardDeny_BuildCmd(t *testing.T) {
	cfg := unattendedOutwardCfg(t)
	groups := disallowedGroups(t, unattendedOutwardLine(t, cfg, true))
	if len(groups) != 2 {
		t.Fatalf("want 2 --disallowedTools flags, got %d", len(groups))
	}
	// The question/plan trio stays on its own flag (claude accumulates
	// repeated --disallowedTools flags; verified headless).
	testutil.DeepEqual(t, groups[0], []string{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode"})
	outward := groups[1]
	testutil.DeepEqual(t, outward, ResidentWorkerDenyRules(cfg))
	for _, tool := range []string{
		"mcp__claude_ai_Gmail__send_message",
		"mcp__claude_ai_Gmail__reply",
		"mcp__claude_ai_Gmail__forward",
		"mcp__claude_ai_Superhuman_Mail__send_draft",
		"mcp__claude_ai_Slack__slack_send_message",
		"mcp__claude_ai_Slack__slack_schedule_message",
		"mcp__claude_ai_Google_Calendar__create_event",
		"mcp__claude_ai_Google_Calendar__update_event",
		"mcp__claude_ai_Google_Calendar__delete_event",
		"mcp__claude_ai_Notion__notion-update-page",
		"mcp__claude_ai_Linear__save_issue",
		"mcp__argus__task_message_send",
		"mcp__argus__hera_new_orchestrator",
	} {
		t.Run("denied/"+tool, func(t *testing.T) { testutil.True(t, slices.Contains(outward, tool)) })
	}
	for _, tool := range []string{
		"mcp__claude_ai_Slack__slack_send_message_draft",
		"mcp__claude_ai_Gmail__create_draft",
		"mcp__claude_ai_Superhuman_Mail__create_or_update_draft",
		"mcp__claude_ai_Slack__slack_read_channel",
		"mcp__claude_ai_Google_Calendar__list_events",
		"mcp__claude_ai_Linear__get_issue",
		"mcp__argus__hera_send",
		"mcp__argus__kb_search",
	} {
		t.Run("allowed/"+tool, func(t *testing.T) {
			for _, g := range groups {
				testutil.False(t, slices.Contains(g, tool))
			}
		})
	}
}

func TestUnattendedOutwardDeny_OrdinaryTaskUnaffected(t *testing.T) {
	cfg := unattendedOutwardCfg(t)
	line := unattendedOutwardLine(t, cfg, false)
	testutil.Equal(t, len(disallowedGroups(t, line)), 0)
	testutil.False(t, strings.Contains(line, "mcp__claude_ai_Gmail__send_message"))
}

func TestUnattendedOutwardDeny_EmptyTiers(t *testing.T) {
	cfg := unattendedOutwardCfg(t)
	cfg.Resident.AskTools, cfg.Resident.DenyTools = nil, nil
	groups := disallowedGroups(t, unattendedOutwardLine(t, cfg, true))
	testutil.DeepEqual(t, groups, [][]string{{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode"}})
}

func TestUnattendedOutwardDeny_FailsClosedWithoutDenyFlag(t *testing.T) {
	cfg := unattendedOutwardCfg(t)
	fam := FamilyOf("claude")
	orig := fam.UnattendedDenyFlag
	fam.UnattendedDenyFlag = nil
	t.Cleanup(func() { fam.UnattendedDenyFlag = orig })
	task := &model.Task{ID: "w", Backend: "claude", Worktree: t.TempDir()}
	_, _, err := BuildCmd(task, cfg, false, func(string) (bool, error) { return true, nil })
	testutil.True(t, err != nil)
	testutil.Contains(t, err.Error(), "cannot deny the resident's outward tools")
}

// Every family eligible for unattended workers must be able to deny the
// outward tiers; BuildCmd would otherwise refuse each of its launches.
func TestUnattendedOutwardDeny_EligibleFamiliesCanDeny(t *testing.T) {
	for _, f := range families {
		if f.UnattendedWorker != nil {
			t.Run(string(f.Kind), func(t *testing.T) { testutil.True(t, f.UnattendedDenyFlag != nil) })
		}
	}
}

func TestClaudeUnattendedDenyFlag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []string
		want  string
	}{
		{"empty", nil, ""},
		{"only malformed", []string{"", "  ", "--dangerously-skip-permissions"}, ""},
		{"quoted", []string{"mcp__a__b", "Bash(git push:*)", "it's"}, ` --disallowedTools 'mcp__a__b' 'Bash(git push:*)' 'it'\''s'`},
		{"skips malformed", []string{"-x", "mcp__a__b"}, ` --disallowedTools 'mcp__a__b'`},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Equal(t, claudeUnattendedDenyFlag(tc.rules), tc.want) })
	}
}

func TestResidentWorkerOrientation_OutwardActionsGoThroughResident(t *testing.T) {
	got := HeraWorkerOrientation("resident", "resident", true)
	for _, part := range []string{
		"Outward actions are unavailable to you",
		"denied at launch",
		"drafts and read-only connector calls still work",
		"send it to the resident via hera_send",
		"confirms it with Aaron",
	} {
		t.Run(part, func(t *testing.T) { testutil.Contains(t, got, part) })
	}
	testutil.False(t, strings.Contains(HeraWorkerOrientation("o", "c"), "Outward actions are unavailable"))
}

// The deny list survives a real `sh` as one argv word per rule.
func TestUnattendedOutwardDeny_RealShellArgv(t *testing.T) {
	cfg := unattendedOutwardCfg(t)
	cfg.Resident.AskTools = append(cfg.Resident.AskTools, "Bash(git push:*)")
	words, err := splitShellWords(unattendedOutwardLine(t, cfg, true))
	testutil.NoError(t, err)
	inner := words[len(words)-1].value
	testutil.True(t, strings.HasPrefix(inner, "claude "))
	out, err := exec.Command("sh", "-c", `printf '%s\n'`+strings.TrimPrefix(inner, "claude")).Output()
	testutil.NoError(t, err)
	argv := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	second := slices.Index(argv[slices.Index(argv, "--disallowedTools")+1:], "--disallowedTools")
	testutil.True(t, second >= 0)
	got := argv[slices.Index(argv, "--disallowedTools")+1+second+1:]
	testutil.DeepEqual(t, got, ResidentWorkerDenyRules(cfg))
}
