package config

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentMemoryTidySchedule(t *testing.T) {
	for _, tc := range []struct {
		value, want string
		bad         bool
	}{
		{"", "30 2 * * *", false}, {"02:30", "30 2 * * *", false},
		{"00:00", "0 0 * * *", false}, {"23:59", "59 23 * * *", false},
		{"off", "", false}, {"2:30", "", true}, {"24:00", "", true}, {"no", "", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			r := ResidentConfig{MemoryTidy: tc.value}
			got, err := r.MemoryTidySchedule()
			testutil.Equal(t, err != nil, tc.bad)
			testutil.Equal(t, got, tc.want)
		})
	}
	testutil.Equal(t, DefaultResidentConfig(100).MemoryTidy, "02:30")
}
