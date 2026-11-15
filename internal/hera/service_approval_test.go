package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentApprovalNoticeUsesSystemTurn(t *testing.T) {
	store := newFakeStore()
	store.addRole(1, "resident")
	store.addBinding(1, "r")
	n := &fakeNotifier{}
	svc := New(store, n)
	svc.Deliver(&db.HeraMessage{ID: 12, FromRoleID: 1, ToRoleID: 1, Body: "[argus] Aaron approved deferred call #4", Tldr: "Call #4 approved"})
	testutil.Equal(t, len(n.notifyCalls), 1)
	testutil.Equal(t, n.notifyCalls[0].text, "[argus] Call #4 approved · read: hera_inbox")
}
