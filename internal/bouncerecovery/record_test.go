package bouncerecovery

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/transcriptstate"
)

func TestRecord_DoesNotRediscoverDeadTeam(t *testing.T) {
	d := testDB(t)
	team(t, d)
	old := Entry{TaskID: "w1", Status: StatusExpired, KilledAt: time.Unix(100, 0), BounceID: "100", Attempts: 3, NoteState: NoteAbandoned}
	testutil.NoError(t, Save(d, old))
	before, err := d.ListMeta("w1", Namespace)
	testutil.NoError(t, err)
	testutil.Equal(t, record(d, []string{"c"}, nil, time.Unix(200, 0), stateOf(nil)), 1)
	after, err := d.ListMeta("w1", Namespace)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, before)
	entries, err := LoadAll(d)
	testutil.NoError(t, err)
	testutil.Nil(t, entries["w2"]) // no evidence this bounce killed it, even without an old ledger
}

func TestRecord_SameBouncePreservesProgress(t *testing.T) {
	d := testDB(t)
	team(t, d)
	now := time.Unix(100, 0)
	record(d, []string{"c", "w1", "w2"}, nil, now, stateOf(nil))
	entries, err := LoadAll(d)
	testutil.NoError(t, err)
	e := entries["w1"]
	e.Attempts = 2
	testutil.NoError(t, SetStatus(d, e, StatusFailed))
	testutil.NoError(t, SetNoteState(d, e, NoteSent))
	before, err := d.ListMeta("w1", Namespace)
	testutil.NoError(t, err)
	testutil.Equal(t, record(d, []string{"c", "w1", "w2"}, nil, now, stateOf(nil)), 0)
	after, err := d.ListMeta("w1", Namespace)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, before)
}

func TestRecord_LedgerLookupFailureSkipsClassification(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Close())
	testutil.Equal(t, record(d, []string{"c"}, nil, time.Unix(100, 0), func(task *model.Task, database *db.DB) (transcriptstate.State, bool) {
		t.Fatal("must not classify without knowing whether recovery is already recorded")
		return transcriptstate.State{}, false
	}), 0)
}
