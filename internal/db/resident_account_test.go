package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentAccountMove(t *testing.T) {
	d := testDB(t)
	res, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)

	_, ok, err := d.ResidentAccountOverride(res.ID)
	testutil.NoError(t, err)
	testutil.False(t, ok)
	testutil.True(t, d.ResidentLastAutoSwitch(res.ID).IsZero())

	ov := ResidentAccountOverride{Account: "work", By: OverrideByAuto}
	testutil.NoError(t, d.SetResidentAccountMove(res.ID, "work", "sid-1", ov))
	got, err := d.Get(res.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Account, "work")
	testutil.Equal(t, got.SessionID, "sid-1")
	testutil.False(t, got.AccountRouted)
	gotOv, ok, err := d.ResidentAccountOverride(res.ID)
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, gotOv.Account, "work")
	testutil.Equal(t, gotOv.By, OverrideByAuto)
	testutil.False(t, gotOv.At == "")
	testutil.True(t, time.Since(d.ResidentLastAutoSwitch(res.ID)) < time.Minute)

	// A manual move leaves the last-auto-switch time alone.
	before := d.ResidentLastAutoSwitch(res.ID)
	testutil.NoError(t, d.SetResidentAccountMove(res.ID, "", "sid-2", ResidentAccountOverride{Account: "default", By: OverrideByManual}))
	testutil.True(t, d.ResidentLastAutoSwitch(res.ID).Equal(before))
	got, _ = d.Get(res.ID)
	testutil.Equal(t, got.Account, "")

	testutil.NoError(t, d.ClearResidentAccountOverride(res.ID))
	_, ok, _ = d.ResidentAccountOverride(res.ID)
	testutil.False(t, ok)
}

func TestResidentAccountMoveRefusesOrdinaryAndMissing(t *testing.T) {
	d := testDB(t)
	task := &model.Task{Name: "plain", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	err := d.SetResidentAccountMove(task.ID, "work", "x", ResidentAccountOverride{Account: "work"})
	testutil.Contains(t, err.Error(), "not the resident")
	testutil.ErrorIs(t, d.SetResidentAccountMove("nope", "work", "x", ResidentAccountOverride{Account: "work"}), ErrTaskNotFound)
}

func TestResidentAccountOverrideUnreadableIsAbsent(t *testing.T) {
	d := testDB(t)
	res, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetMeta(res.ID, ResidentMetaNamespace, MetaKeyAccountOverride, "not json"))
	_, ok, err := d.ResidentAccountOverride(res.ID)
	testutil.NoError(t, err)
	testutil.False(t, ok)
}
