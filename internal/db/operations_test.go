package db

import (
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func newOp(id, req, fp, subject string) *Operation {
	return &Operation{ID: id, RequestID: req, Fingerprint: fp, Kind: "bounce", SubjectType: "hera_role", SubjectID: subject, TaskID: "t1", State: "awaiting_handoff"}
}

func TestOperations(t *testing.T) {
	t.Run("create replay reuse and active conflict", func(t *testing.T) {
		d := testDB(t)
		op, created, err := d.CreateOperation(newOp("o1", "r1", "fp", "1"))
		testutil.NoError(t, err)
		testutil.True(t, created)
		testutil.True(t, op.Active)

		again, created, err := d.CreateOperation(newOp("o2", "r1", "fp", "1"))
		testutil.NoError(t, err)
		testutil.True(t, !created)
		testutil.Equal(t, again.ID, "o1")

		_, _, err = d.CreateOperation(newOp("o3", "r1", "other", "1"))
		testutil.ErrorIs(t, err, ErrOperationRequestReuse)

		_, _, err = d.CreateOperation(newOp("o4", "r2", "fp", "1"))
		testutil.ErrorIs(t, err, ErrOperationActive)

		_, created, err = d.CreateOperation(newOp("o5", "r3", "fp", "2"))
		testutil.NoError(t, err)
		testutil.True(t, created)
	})

	t.Run("transition is compare-and-set and frees the subject", func(t *testing.T) {
		d := testDB(t)
		_, _, err := d.CreateOperation(newOp("o1", "r1", "fp", "1"))
		testutil.NoError(t, err)

		op, changed, err := d.TransitionOperation("o1", "awaiting_handoff", "restarted", "", false)
		testutil.NoError(t, err)
		testutil.True(t, changed)
		testutil.Equal(t, op.State, "restarted")
		testutil.True(t, !op.Active)

		_, changed, err = d.TransitionOperation("o1", "awaiting_handoff", "failed", "x", false)
		testutil.NoError(t, err)
		testutil.True(t, !changed)

		_, created, err := d.CreateOperation(newOp("o2", "r2", "fp", "1"))
		testutil.NoError(t, err)
		testutil.True(t, created)
	})

	t.Run("lookups and listing", func(t *testing.T) {
		d := testDB(t)
		_, _, err := d.CreateOperation(newOp("o1", "r1", "fp", "1"))
		testutil.NoError(t, err)
		_, err = d.Operation("missing")
		testutil.ErrorIs(t, err, ErrOperationNotFound)
		_, err = d.OperationByRequestID("missing")
		testutil.ErrorIs(t, err, ErrOperationNotFound)
		got, err := d.ActiveOperation("hera_role", "1", "bounce")
		testutil.NoError(t, err)
		testutil.Equal(t, got.ID, "o1")
		list, err := d.ListActiveOperations("bounce", "awaiting_handoff")
		testutil.NoError(t, err)
		testutil.Equal(t, len(list), 1)
	})
}
