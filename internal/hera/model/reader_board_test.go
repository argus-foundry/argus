package model

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentReaderForwardsOptionalBoard(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	roles, err := d.ListHeraRoles(o.ID, false)
	testutil.NoError(t, err)
	item, err := d.PostHeraBoardItem(roles[0].ID, "fyi", "Reminder", "Body")
	testutil.NoError(t, err)
	r := NewResidentReader(d, d)
	items, err := r.ListHeraBoardItems(roles[0].ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].ID, item.ID)
	testutil.NoError(t, d.ResolveHeraBoardItem(roles[0].ID, item.ID, "human", ""))
	items, err = r.ListHeraBoardItems(roles[0].ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 0)
	items, err = r.ListHeraBoardItems(roles[0].ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.True(t, items[0].Resolved)
	// A legacy reader without optional board support still degrades to empty.
	narrow := struct{ HeraReader }{d}
	items, err = NewResidentReader(narrow, d).ListHeraBoardItems(roles[0].ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 0)
	testutil.NoError(t, d.Close())
	_, err = r.ListHeraBoardItems(roles[0].ID, false)
	testutil.True(t, err != nil)
}
