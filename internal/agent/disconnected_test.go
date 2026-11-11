package agent

import (
	"sort"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func TestDisconnectedIDs(t *testing.T) {
	t.Run("absent task needs two consecutive ticks", func(t *testing.T) {
		ids, next := DisconnectedIDs([]string{"a"}, nil, true, nil)
		testutil.Equal(t, len(ids), 0)
		ids, _ = DisconnectedIDs([]string{"a"}, nil, true, next)
		testutil.DeepEqual(t, ids, []string{"a"})
	})
	t.Run("running task never disconnected and resets the count", func(t *testing.T) {
		_, next := DisconnectedIDs([]string{"a"}, nil, true, nil)
		ids, next := DisconnectedIDs([]string{"a"}, []string{"a"}, true, next)
		testutil.Equal(t, len(ids), 0)
		testutil.Equal(t, len(next), 0)
		ids, _ = DisconnectedIDs([]string{"a"}, nil, true, next)
		testutil.Equal(t, len(ids), 0) // count restarted
	})
	t.Run("unhealthy list marks nothing new and keeps state", func(t *testing.T) {
		_, next := DisconnectedIDs([]string{"a", "b"}, []string{"b"}, true, nil)
		ids, next2 := DisconnectedIDs([]string{"a", "b"}, nil, false, next)
		testutil.Equal(t, len(ids), 0) // a only reached count 1
		testutil.DeepEqual(t, next2, next)
	})
	t.Run("unhealthy list keeps an already-disconnected task disconnected", func(t *testing.T) {
		_, n := DisconnectedIDs([]string{"a"}, nil, true, nil)
		_, n = DisconnectedIDs([]string{"a"}, nil, true, n)
		ids, _ := DisconnectedIDs([]string{"a"}, nil, false, n)
		testutil.DeepEqual(t, ids, []string{"a"})
	})
	t.Run("task no longer a candidate is dropped", func(t *testing.T) {
		_, n := DisconnectedIDs([]string{"a"}, nil, true, nil)
		ids, next := DisconnectedIDs(nil, nil, true, n)
		testutil.Equal(t, len(ids), 0)
		testutil.Equal(t, len(next), 0)
	})
	t.Run("multiple ids", func(t *testing.T) {
		_, n := DisconnectedIDs([]string{"a", "b", "c"}, []string{"c"}, true, nil)
		ids, _ := DisconnectedIDs([]string{"a", "b", "c"}, []string{"c"}, true, n)
		testutil.DeepEqual(t, sorted(ids), []string{"a", "b"})
	})
}
