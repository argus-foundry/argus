package db

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSetTaskAccountMove(t *testing.T) {
	d := testDB(t)
	task := &model.Task{Name: "moving", SessionID: "old", Status: model.StatusInProgress, AccountRouted: true, Prompt: "original", Model: "chosen"}
	testutil.NoError(t, d.Add(task))
	before, err := d.Get(task.ID)
	testutil.NoError(t, err)
	before.Name = "renamed"
	before.SetStatus(model.StatusInReview)
	testutil.NoError(t, d.Update(before))
	for i, session := range []string{"captured", "captured", ""} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			testutil.NoError(t, d.SetTaskAccountMove(task.ID, "work", false, session))
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "work")
			testutil.Equal(t, got.SessionID, session)
			testutil.False(t, got.AccountRouted)
			testutil.Equal(t, got.Status, model.StatusInReview)
			testutil.Equal(t, got.Name, "renamed")
			testutil.Equal(t, got.Prompt, "original")
			testutil.Equal(t, got.Model, "chosen")
		})
	}
	testutil.ErrorIs(t, d.SetTaskAccountMove("missing", "work", false, "id"), ErrTaskNotFound)
	resident, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	testutil.ErrorIs(t, d.SetTaskAccountMove(resident.ID, "work", false, "id"), ErrResidentProtected)
}
