package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardZeroExpectedRevision(t *testing.T) {
	for _, action := range []struct{ method, path string }{
		{"PUT", "read-state"}, {"POST", "dismiss"},
	} {
		t.Run(action.path, func(t *testing.T) {
			for _, revision := range []string{"0001-01-01T00:00:00Z", "0001-01-01T01:00:00+01:00"} {
				t.Run(revision, func(t *testing.T) {
					s, d, c, _, item := boardRESTFixture(t)
					edited, err := d.UpdateHeraBoardItem(c.ID, item.ID, "decision", "Edited", "unseen content")
					testutil.NoError(t, err)
					sink := &boardSink{}
					prev := events.SetSink(sink)
					t.Cleanup(func() { events.SetSink(prev) })
					body := fmt.Sprintf(`{"read":true,"expected_updated_at":%q,"note":"stop"}`, revision)
					rr := doBoard(s, action.method, fmt.Sprintf("/api/hera/roles/%d/board/%d/%s", c.ID, item.ID, action.path), body)
					testutil.Equal(t, rr.Code, 400)
					testutil.Equal(t, codeOf(t, rr), "invalid_request")
					items, err := d.ListHeraBoardItems(c.ID, true)
					testutil.NoError(t, err)
					testutil.Equal(t, len(items), 1)
					testutil.DeepEqual(t, items[0], edited)
					unread, _, err := d.HeraBoardUnread(c.ID)
					testutil.NoError(t, err)
					testutil.Equal(t, unread, 1)
					notices, err := d.HeraInbox(c.ID)
					testutil.NoError(t, err)
					testutil.Equal(t, len(notices), 0)
					testutil.Equal(t, sink.count(), 0)
				})
			}
		})
	}
}

func TestBoardMutationConsumesBoundedBody(t *testing.T) {
	for _, action := range []struct {
		method, path string
		limit        int
	}{
		{"PUT", "read-state", 80 * 1024}, {"POST", "dismiss", 394240},
	} {
		t.Run(action.path, func(t *testing.T) {
			for _, tc := range []struct {
				name, suffix    string
				padding, status int
			}{
				{name: "garbage", suffix: "garbage", status: 400},
				{name: "second object", suffix: ` {}`, status: 400},
				{name: "second scalar", suffix: ` true`, status: 400},
				{name: "second null", suffix: ` null`, status: 400},
				{name: "allowed whitespace", suffix: " \n\t", status: 200},
				{name: "at allowance", padding: action.limit, status: 200},
				{name: "over allowance", padding: action.limit + 1, status: 400},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s, d, c, _, item := boardRESTFixture(t)
					sink := &boardSink{}
					prev := events.SetSink(sink)
					t.Cleanup(func() { events.SetSink(prev) })
					body := fmt.Sprintf(`{"read":true,"expected_updated_at":%q,"note":"stop"}`, ts(item.UpdatedAt))
					if tc.padding > 0 {
						body += strings.Repeat(" ", tc.padding-len(body))
					} else {
						body += tc.suffix
					}
					rr := doBoard(s, action.method, fmt.Sprintf("/api/hera/roles/%d/board/%d/%s", c.ID, item.ID, action.path), body)
					testutil.Equal(t, rr.Code, tc.status)
					items, err := d.ListHeraBoardItems(c.ID, true)
					testutil.NoError(t, err)
					testutil.Equal(t, len(items), 1)
					notices, err := d.HeraInbox(c.ID)
					testutil.NoError(t, err)
					if tc.status == 400 {
						testutil.Equal(t, codeOf(t, rr), "invalid_request")
						testutil.DeepEqual(t, items[0], item)
						testutil.Equal(t, len(notices), 0)
						testutil.Equal(t, sink.count(), 0)
					} else {
						testutil.Equal(t, items[0].Read, action.path == "read-state")
						testutil.Equal(t, items[0].Resolved, action.path == "dismiss")
						count := 0
						if action.path == "dismiss" {
							count = 1
							testutil.Equal(t, items[0].ResolutionNote, "stop")
						}
						testutil.Equal(t, len(notices), count)
						testutil.Equal(t, sink.count(), 1)
					}
				})
			}
		})
	}
}
