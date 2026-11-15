package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPermissionPromptRendered(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		permission bool
	}{
		{"plain question", "Do you want to proceed?\n❯ 1. Yes\n2. No", false},
		{"cursor addressed", "\x1b[2J\x1b[HBash\x1b[1;6Hcommand\x1b[2;1HDo\x1b[2;4Hyou\x1b[2;8Hwant\x1b[2;13Hto\x1b[2;16Hproceed?\x1b[3;1H❯ 1. Yes", true},
		{"question", "Which option?\n❯ 1. React\n2. Vue", false},
		{"no dialog", "Working...", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, NewTailView(&ScreenRenderer{}, []byte(tc.tail), 80, 24).PermissionPrompt(), tc.permission)
		})
	}
	testutil.True(t, NewTailView(nil, []byte("Permission rule"), 80, 24).PermissionPrompt())
}
