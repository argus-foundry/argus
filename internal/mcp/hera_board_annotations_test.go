package mcp

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardToolAnnotationGuidance(t *testing.T) {
	for _, tool := range boardToolDefs() {
		t.Run(tool.Name, func(t *testing.T) {
			for _, rule := range []string{"address every annotation", "COMMENT", "DELETION", "suggestion", "fyi-actions", "comment-only decisions remain open"} {
				testutil.Contains(t, tool.Description, rule)
			}
		})
	}
}
