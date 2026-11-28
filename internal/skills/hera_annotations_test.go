package skills

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBuiltinHeraAnnotationGuidance(t *testing.T) {
	raw, err := os.ReadFile("builtin/hera/SKILL.md")
	testutil.NoError(t, err)
	for _, rule := range []string{"address every annotation", "COMMENT", "DELETION", "suggestion", "fyi-actions", "comment-only decisions remain open"} {
		t.Run(rule, func(t *testing.T) { testutil.Contains(t, string(raw), rule) })
	}
}
