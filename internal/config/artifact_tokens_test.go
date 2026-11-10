package config

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestArtifactTokens_ParseAndMerge(t *testing.T) {
	path := writeFile(t, `
[artifact_tokens]
personal = "op://claude/shell-env/CLAUDE_SETUP_TOKEN_PERSONAL"
default = "keychain://argus-artifacts/default"
`)
	base := Config{ArtifactTokens: map[string]string{"base": "env://BASE_TOKEN"}}
	l := NewFileLoader(path)
	got := l.Apply(base)
	testutil.NoError(t, l.Err())

	t.Run("decodes the table", func(t *testing.T) {
		testutil.Equal(t, got.ArtifactTokens["personal"], "op://claude/shell-env/CLAUDE_SETUP_TOKEN_PERSONAL")
		testutil.Equal(t, got.ArtifactTokens["default"], "keychain://argus-artifacts/default")
	})
	t.Run("merges over base entries", func(t *testing.T) {
		testutil.Equal(t, got.ArtifactTokens["base"], "env://BASE_TOKEN")
	})
	t.Run("base map is not mutated", func(t *testing.T) {
		testutil.Equal(t, len(base.ArtifactTokens), 1)
		_, ok := base.ArtifactTokens["personal"]
		testutil.False(t, ok)
	})
}

func TestArtifactTokenSource(t *testing.T) {
	literal := "sk-ant-oat01-literal-secret-value"
	cfg := Config{ArtifactTokens: map[string]string{
		"personal":  "op://claude/shell-env/CLAUDE_SETUP_TOKEN_PERSONAL",
		"kc":        "keychain://argus/kc",
		"env":       "env://ARTIFACT_TOKEN",
		"literal":   literal,
		"blank":     "  ",
		"badscheme": "https://example.com/token",
	}}
	for _, tc := range []struct {
		name, account, want string
		wantErr             error
		wantMsg             string
	}{
		{name: "op reference", account: "personal", want: "op://claude/shell-env/CLAUDE_SETUP_TOKEN_PERSONAL"},
		{name: "keychain reference", account: "kc", want: "keychain://argus/kc"},
		{name: "env reference", account: "env", want: "env://ARTIFACT_TOKEN"},
		{name: "missing entry", account: "work", wantErr: ErrNoArtifactToken, wantMsg: "[artifact_tokens]"},
		{name: "literal token rejected", account: "literal", wantErr: ErrArtifactTokenNotReference, wantMsg: "not a secret reference"},
		{name: "blank rejected", account: "blank", wantErr: ErrArtifactTokenNotReference},
		{name: "unknown scheme rejected", account: "badscheme", wantErr: ErrArtifactTokenNotReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cfg.ArtifactTokenSource(tc.account)
			if tc.wantErr != nil {
				testutil.ErrorIs(t, err, tc.wantErr)
				testutil.Contains(t, err.Error(), tc.account)
				if tc.wantMsg != "" {
					testutil.Contains(t, err.Error(), tc.wantMsg)
				}
				// The rejected value itself is never echoed: it may be a pasted token.
				testutil.False(t, strings.Contains(err.Error(), literal))
				testutil.Equal(t, got, "")
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, got, tc.want)
		})
	}
}

func TestArtifactTokenAccounts_Sorted(t *testing.T) {
	cfg := Config{ArtifactTokens: map[string]string{"zeta": "env://Z", "alpha": "env://A", "mid": "env://M"}}
	testutil.DeepEqual(t, cfg.ArtifactTokenAccounts(), []string{"alpha", "mid", "zeta"})
	testutil.Equal(t, len(Config{}.ArtifactTokenAccounts()), 0)
}

func TestIsSecretReference(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"op://v/i/f", true},
		{"keychain://svc", true},
		{"env://VAR", true},
		{"op://", false},
		{"VAR", false},
		{"https://x", false},
		{"", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			testutil.Equal(t, IsSecretReference(tc.in), tc.want)
		})
	}
}
