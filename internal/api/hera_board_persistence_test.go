package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardPersistenceFailures(t *testing.T) {
	for _, tc := range []struct{ name, method, action, setup string }{
		{"read update", "PUT", "read-state", `CREATE TRIGGER fail_update BEFORE UPDATE ON hera_board_items BEGIN SELECT RAISE(ABORT, 'update failed'); END`},
		{"dismiss update", "POST", "dismiss", `CREATE TRIGGER fail_update BEFORE UPDATE ON hera_board_items BEGIN SELECT RAISE(ABORT, 'update failed'); END`},
		{"dismiss notice", "POST", "dismiss", `CREATE TRIGGER fail_notice BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT, 'notice failed'); END`},
		{"dismiss commit", "POST", "dismiss", `CREATE TABLE commit_guard (role_id INTEGER REFERENCES hera_roles(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER fail_commit AFTER INSERT ON hera_messages BEGIN INSERT INTO commit_guard VALUES(-1); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "board.sql")
			d, err := db.Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(on)")
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			s := New(d, agent.NewRunner(nil), "test-token", nil, nil)
			o, err := d.CreateHeraOrchestrator("o", "")
			testutil.NoError(t, err)
			c, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
			testutil.NoError(t, err)
			item, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "original body")
			testutil.NoError(t, err)
			_, err = conn.Exec(tc.setup)
			testutil.NoError(t, err)
			sink := &boardSink{}
			prev := events.SetSink(sink)
			t.Cleanup(func() { events.SetSink(prev) })
			body := fmt.Sprintf(`{"read":true,"expected_updated_at":%q,"note":"stop"}`, ts(item.UpdatedAt))
			rr := doBoard(s, tc.method, fmt.Sprintf("/api/hera/roles/%d/board/%d/%s", c.ID, item.ID, tc.action), body)
			testutil.Equal(t, rr.Code, 500)
			var response map[string]any
			testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
			testutil.Equal(t, response["code"], any("board_write_failed"))
			testutil.Equal(t, response["error"], any("board write failed"))
			_, hasItem := response["item"]
			_, hasCount := response["unread_count"]
			testutil.Equal(t, hasItem, false)
			testutil.Equal(t, hasCount, false)
			testutil.Equal(t, sink.count(), 0)
			var read, resolved bool
			var unread, notices int
			testutil.NoError(t, conn.QueryRow(`SELECT is_read,resolved FROM hera_board_items WHERE id=?`, item.ID).Scan(&read, &resolved))
			testutil.Equal(t, read, false)
			testutil.Equal(t, resolved, false)
			testutil.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM hera_board_items WHERE role_id=? AND is_read=0 AND resolved=0`, c.ID).Scan(&unread))
			testutil.Equal(t, unread, 1)
			testutil.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM hera_messages WHERE to_role_id=?`, c.ID).Scan(&notices))
			testutil.Equal(t, notices, 0)
		})
	}
}

func TestBoardMutationFullItem(t *testing.T) {
	s, d, c, _, expected := boardRESTFixture(t)
	for _, tc := range []struct {
		name, method, action string
		read                 bool
	}{
		{"read", "PUT", "read-state", true},
		{"unread", "PUT", "read-state", false},
		{"dismiss", "POST", "dismiss", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"read":%t,"expected_updated_at":%q,"note":"original note"}`, tc.read, ts(expected.UpdatedAt))
			rr := doBoard(s, tc.method, fmt.Sprintf("/api/hera/roles/%d/board/%d/%s", c.ID, expected.ID, tc.action), body)
			testutil.Equal(t, rr.Code, 200)
			var response struct {
				Item   db.HeraBoardItem `json:"item"`
				Unread int              `json:"unread_count"`
			}
			testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
			testutil.Equal(t, response.Item.ID, expected.ID)
			testutil.Equal(t, response.Item.RoleID, c.ID)
			testutil.Equal(t, response.Item.Kind, expected.Kind)
			testutil.Equal(t, response.Item.Subject, expected.Subject)
			testutil.Equal(t, response.Item.Body, expected.Body)
			testutil.Equal(t, response.Item.CreatedAt.Equal(expected.CreatedAt), true)
			testutil.Equal(t, response.Item.Read, tc.read)
			testutil.Equal(t, response.Item.Resolved, tc.action == "dismiss")
			if tc.action == "dismiss" {
				testutil.Equal(t, response.Item.ResolvedBy, "human")
				testutil.Equal(t, response.Item.ResolutionNote, "original note")
				stored, err := d.ListHeraBoardItems(c.ID, true)
				testutil.NoError(t, err)
				testutil.Equal(t, response.Item.UpdatedAt.Equal(stored[0].UpdatedAt), true)
				testutil.Equal(t, response.Item.UpdatedAt.Before(expected.UpdatedAt), false)
				testutil.Equal(t, response.Unread, 0)
			} else {
				testutil.Equal(t, response.Item.ResolvedBy, "")
				testutil.Equal(t, response.Item.ResolutionNote, "")
				testutil.Equal(t, response.Item.UpdatedAt.Equal(expected.UpdatedAt), true)
				wantUnread := 1
				if tc.read {
					wantUnread = 0
				}
				testutil.Equal(t, response.Unread, wantUnread)
			}
		})
	}
}
