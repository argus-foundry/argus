package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSetSessionIDIfEmpty(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{Name: "t", Project: "p"}
	testutil.NoError(t, d.Add(task))

	wrote, err := d.SetSessionIDIfEmpty(task.ID, "first")
	testutil.NoError(t, err)
	testutil.True(t, wrote)
	wrote, err = d.SetSessionIDIfEmpty(task.ID, "second")
	testutil.NoError(t, err)
	testutil.True(t, !wrote)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.SessionID, "first")

	_, err = d.SetSessionIDIfEmpty("missing", "x")
	testutil.ErrorIs(t, err, ErrTaskNotFound)
	_, err = d.SetSessionIDIfEmpty("", "x")
	testutil.Error(t, err)
}
