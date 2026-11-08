package db

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

func TestSetHeraBoardItemReadConcurrentWriter(t *testing.T) {
	for _, tc := range []struct {
		name, mutation string
		want           error
		remaining      int
	}{
		{"edited", `UPDATE hera_board_items SET body='independent edit',updated_at=? WHERE id=?`, ErrHeraBoardConflict, 1},
		{"resolved", `UPDATE hera_board_items SET resolved=1,resolved_by='agent',updated_at=? WHERE id=?`, ErrHeraBoardResolved, 1},
		{"deleted", `DELETE FROM hera_board_items WHERE updated_at<>? AND id=?`, ErrHeraBoardNotFound, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "board.sql")
			d, err := Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			o := mkOrch(t, d, "o")
			c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
			item, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "original")
			testutil.NoError(t, err)
			writer, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = writer.Close() })
			tx, err := writer.Begin()
			testutil.NoError(t, err)
			_, err = tx.Exec(tc.mutation, formatTime(item.UpdatedAt.Add(time.Second)), item.ID)
			testutil.NoError(t, err)
			sink := sinkFor(t)
			var got *HeraBoardItem
			var unread int
			var readErr error
			finished := make(chan struct{})
			go func() {
				got, unread, readErr = d.SetHeraBoardItemRead(c.ID, item.ID, true, item.UpdatedAt)
				close(finished)
			}()
			t.Cleanup(func() { _ = tx.Rollback(); <-finished })
			awaitBoardReadUpdate(t, finished)
			testutil.NoError(t, tx.Commit())
			<-finished
			testutil.ErrorIs(t, readErr, tc.want)
			testutil.Nil(t, got)
			testutil.Equal(t, unread, 0)
			testutil.Equal(t, len(sink.board()), 0)
			var remaining int
			testutil.NoError(t, writer.QueryRow(`SELECT COUNT(*) FROM hera_board_items WHERE id=?`, item.ID).Scan(&remaining))
			testutil.Equal(t, remaining, tc.remaining)
			if tc.remaining != 0 {
				var read, resolved bool
				var body, revision string
				testutil.NoError(t, writer.QueryRow(`SELECT is_read,resolved,body,updated_at FROM hera_board_items WHERE id=?`, item.ID).Scan(&read, &resolved, &body, &revision))
				testutil.Equal(t, read, false)
				testutil.Equal(t, resolved, tc.name == "resolved")
				testutil.Equal(t, revision, formatTime(item.UpdatedAt.Add(time.Second)))
				if tc.name == "edited" {
					testutil.Equal(t, body, "independent edit")
				}
			}
		})
	}
}

// The writer lock keeps UPDATE pending while WAL readers can still see the old revision.
func awaitBoardReadUpdate(t *testing.T, finished <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	stack := make([]byte, 1<<20)
	for {
		select {
		case <-finished:
			t.Fatal("read-state call finished before reaching the blocked UPDATE")
		default:
		}
		n := runtime.Stack(stack, true)
		for _, goroutine := range strings.Split(string(stack[:n]), "\n\n") {
			if strings.Contains(goroutine, "setHeraBoardItemRead") && strings.Contains(goroutine, "database/sql.(*DB).exec") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("read-state call did not reach the blocked UPDATE")
		}
		runtime.Gosched()
	}
}

func TestHeraBoardResolveConcurrentWriter(t *testing.T) {
	for _, human := range []bool{false, true} {
		name := "agent resolve"
		if human {
			name = "human dismiss"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "board.sql")
			d, err := Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			o := mkOrch(t, d, "o")
			c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
			item, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "original")
			testutil.NoError(t, err)
			writer, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = writer.Close() })
			tx, err := writer.Begin()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			_, err = tx.Exec(`UPDATE hera_board_items SET body='independent edit',updated_at=? WHERE id=?`, formatTime(item.UpdatedAt.Add(time.Second)), item.ID)
			testutil.NoError(t, err)
			sink := sinkFor(t)
			if human {
				var got *HeraBoardItem
				var unread int
				got, unread, err = d.DismissHeraBoardItem(c.ID, item.ID, item.UpdatedAt, "stop")
				testutil.Nil(t, got)
				testutil.Equal(t, unread, 0)
			} else {
				err = d.ResolveHeraBoardItem(c.ID, item.ID, "agent", "")
			}
			testutil.Error(t, err)
			testutil.Equal(t, len(sink.board()), 0)
			testutil.NoError(t, tx.Commit())
			var body string
			var read, resolved bool
			testutil.NoError(t, writer.QueryRow(`SELECT body,is_read,resolved FROM hera_board_items WHERE id=?`, item.ID).Scan(&body, &read, &resolved))
			testutil.Equal(t, body, "independent edit")
			testutil.Equal(t, read, false)
			testutil.Equal(t, resolved, false)
			var notices int
			testutil.NoError(t, writer.QueryRow(`SELECT COUNT(*) FROM hera_messages WHERE to_role_id=?`, c.ID).Scan(&notices))
			testutil.Equal(t, notices, 0)
		})
	}
}
