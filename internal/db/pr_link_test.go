package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestManualPR_Lifecycle(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "t1", Name: "n"}))
	url := "https://github.com/o/r/pull/1"

	testutil.ErrorIs(t, d.SetManualPR("missing", url), ErrTaskNotFound)
	_, err := d.ClearManualPR("missing")
	testutil.ErrorIs(t, err, ErrTaskNotFound)

	testutil.NoError(t, d.SetMetaBatch("t1", "pr", map[string]string{"state": "approved"}))
	testutil.NoError(t, d.SetManualPR("t1", url))
	got, err := d.ManualPR("t1")
	testutil.NoError(t, err)
	testutil.Equal(t, got, url)
	all, err := d.ManualPRs()
	testutil.NoError(t, err)
	testutil.DeepEqual(t, all, map[string]string{"t1": url})
	meta, _ := d.ListMeta("t1", "pr")
	testutil.Equal(t, len(meta), 0)

	t.Run("archive keeps association", func(t *testing.T) {
		testutil.NoError(t, d.SetArchived("t1", true))
		got, err := d.ManualPR("t1")
		testutil.NoError(t, err)
		testutil.Equal(t, got, url)
	})

	t.Run("auto write refused, manual write accepted", func(t *testing.T) {
		wrote, err := d.SetAutoPRMetaIfChanged("t1", map[string]string{"state": "draft"})
		testutil.ErrorIs(t, err, ErrPRLinkChanged)
		testutil.Equal(t, wrote, false)
		wrote, err = d.SetManualPRMetaIfChanged("t1", "https://github.com/o/r/pull/2", map[string]string{"state": "draft"})
		testutil.ErrorIs(t, err, ErrPRLinkChanged)
		testutil.Equal(t, wrote, false)
		wrote, err = d.SetManualPRMetaIfChanged("t1", url, map[string]string{"state": "draft"})
		testutil.NoError(t, err)
		testutil.Equal(t, wrote, true)
		wrote, err = d.SetManualPRMetaIfChanged("t1", url, map[string]string{"state": "draft"})
		testutil.NoError(t, err)
		testutil.Equal(t, wrote, false)
	})

	t.Run("clear then auto write accepted", func(t *testing.T) {
		changed, err := d.ClearManualPR("t1")
		testutil.NoError(t, err)
		testutil.Equal(t, changed, true)
		got, _ := d.ManualPR("t1")
		testutil.Equal(t, got, "")
		wrote, err := d.SetAutoPRMetaIfChanged("t1", map[string]string{"state": "draft"})
		testutil.NoError(t, err)
		testutil.Equal(t, wrote, true)
	})

	t.Run("delete removes association", func(t *testing.T) {
		testutil.NoError(t, d.SetManualPR("t1", url))
		testutil.NoError(t, d.Delete("t1"))
		all, err := d.ManualPRs()
		testutil.NoError(t, err)
		testutil.Equal(t, len(all), 0)
	})
}
