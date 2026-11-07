package db

import (
	"testing"

	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func TestResultAt_StampedOnChangeOnly(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "r1", Name: "n"}))
	testutil.Equal(t, d.ResultAt("r1").IsZero(), true)

	testutil.NoError(t, d.SetResult("r1", `{"failed":true}`))
	first := d.ResultAt("r1")
	testutil.Equal(t, first.IsZero(), false)

	testutil.NoError(t, d.SetResult("r1", `{"failed":true}`))
	testutil.Equal(t, d.ResultAt("r1").Equal(first), true)

	testutil.NoError(t, d.SetResult("r1", `{"failed":true,"reason":"x"}`))
	testutil.Equal(t, d.ResultAt("r1").After(first) || d.ResultAt("r1").Equal(first), true)

	times, err := d.ResultTimes()
	testutil.NoError(t, err)
	testutil.Equal(t, len(times), 1)
}

func TestResultAt_SurvivesArchive(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "r2", Name: "n"}))
	testutil.NoError(t, d.SetResult("r2", `{"failed":true}`))
	at := d.ResultAt("r2")
	testutil.NoError(t, d.SetArchived("r2", true))
	testutil.Equal(t, d.ResultAt("r2").Equal(at), true)
}
