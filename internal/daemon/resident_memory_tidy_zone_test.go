package daemon

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentMemoryTidyZone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tz, link, want string
		bad                  bool
	}{
		{"environment", "America/New_York", "/usr/share/zoneinfo/Europe/London", "America/New_York", false},
		{"explicit UTC", "UTC", "", "UTC", false},
		{"symlink", "", "/var/db/timezone/zoneinfo/America/Los_Angeles", "America/Los_Angeles", false},
		{"invalid environment falls back", "wrong", "/usr/share/zoneinfo/Europe/London", "Europe/London", false},
		{"Local falls back", "Local", "/usr/share/zoneinfo/Europe/London", "Europe/London", false},
		{"unresolved", "wrong", "/not-zoneinfo", "", true},
		{"Local symlink refused", "", "/usr/share/zoneinfo/Local", "", true},
		{"invalid symlink", "", "/usr/share/zoneinfo/wrong", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveResidentMemoryTidyZone(tc.tz, func(string) (string, error) { return tc.link, nil })
			testutil.Equal(t, err != nil, tc.bad)
			testutil.Equal(t, got, tc.want)
		})
	}
	_, err := resolveResidentMemoryTidyZone("", func(string) (string, error) { return "", errors.New("not a symlink") })
	testutil.NotNil(t, err)
}
