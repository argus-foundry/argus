package config

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestValidateNewAccountName(t *testing.T) {
	existing := map[string]Account{"work": {ClaudeConfigDir: "/tmp/w"}}
	for _, tc := range []struct {
		name    string
		wantErr bool
	}{
		{"personal", false},
		{"a-b_1", false},
		{"", true},
		{"a b", true},
		{"default", true},
		{"DEFAULT", true},
		{"Work", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewAccountName(tc.name, existing)
			testutil.Equal(t, err != nil, tc.wantErr)
		})
	}
}

func TestValidateAccountEdit(t *testing.T) {
	existing := map[string]Account{"work": {ClaudeConfigDir: "/tmp/w", CodexHome: "/tmp/wc"}}
	for _, tc := range []struct {
		name    string
		acct    string
		a       Account
		wantErr bool
	}{
		{"new valid", "p", Account{ClaudeConfigDir: "/tmp/p"}, false},
		{"relative dir", "p", Account{ClaudeConfigDir: "p"}, true},
		{"no dirs", "p", Account{}, true},
		{"shares claude dir", "p", Account{ClaudeConfigDir: "/tmp/w/"}, true},
		{"shares codex dir", "p", Account{CodexHome: "/tmp/wc"}, true},
		{"editing itself keeps its dir", "work", Account{ClaudeConfigDir: "/tmp/w", Label: "x"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAccountEdit(existing, tc.acct, tc.a)
			testutil.Equal(t, err != nil, tc.wantErr)
		})
	}
	t.Run("nil existing map", func(t *testing.T) {
		testutil.NoError(t, ValidateAccountEdit(nil, "p", Account{ClaudeConfigDir: "/tmp/p"}))
	})
}
