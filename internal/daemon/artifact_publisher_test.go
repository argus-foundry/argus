package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/buildid"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestNewArtifactPublisher(t *testing.T) {
	cfg := config.Config{ArtifactTokens: map[string]string{"personal": "env://ARGUS_TEST_ARTIFACT_TOKEN"}}
	p := newArtifactPublisher(func() config.Config { return cfg })
	testutil.Equal(t, p.Config().ArtifactTokens["personal"], "env://ARGUS_TEST_ARTIFACT_TOKEN")
	testutil.True(t, p.Resolve != nil)
	testutil.True(t, p.Client != nil)
	testutil.Contains(t, p.Client.UserAgent, "argus")
}

func TestArtifactUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   buildid.VCS
		want string
	}{
		{"no vcs", buildid.VCS{}, "argus"},
		{"short sha", buildid.VCS{Revision: "abc123"}, "argus/abc123"},
		{"full sha truncated", buildid.VCS{Revision: "0123456789abcdef0123"}, "argus/0123456789ab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, artifactUserAgent(tc.in), tc.want)
		})
	}
}
