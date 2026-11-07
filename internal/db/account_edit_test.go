package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUpdateAccountFields(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.SetAccount("work", config.Account{Label: "old", ClaudeConfigDir: "/claude", CodexHome: "/codex"}))
	for _, tc := range []struct {
		name   string
		fields map[string]string
		want   config.Account
	}{
		{"label", map[string]string{"label": "new"}, config.Account{Label: "new", ClaudeConfigDir: "/claude", CodexHome: "/codex"}},
		{"directories", map[string]string{"claude_config_dir": "/other", "codex_home": ""}, config.Account{Label: "new", ClaudeConfigDir: "/other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.NoError(t, d.UpdateAccountFields("work", tc.fields))
			rows, err := d.Accounts()
			testutil.NoError(t, err)
			testutil.DeepEqual(t, rows["work"], tc.want)
		})
	}
	for _, tc := range []struct {
		name   string
		fields map[string]string
	}{
		{"empty", nil}, {"unknown", map[string]string{"name": "rename"}}, {"sql identifier", map[string]string{"label = 'bad'; --": "bad"}},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.True(t, d.UpdateAccountFields("work", tc.fields) != nil) })
	}
	testutil.ErrorIs(t, d.UpdateAccountFields("missing", map[string]string{"label": "x"}), sql.ErrNoRows)
	testutil.NoError(t, d.Close())
	testutil.True(t, d.UpdateAccountFields("work", map[string]string{"label": "x"}) != nil)
}

func TestUpdateAccountFields_IndependentConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sql")
	first, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	testutil.NoError(t, first.SetAccount("work", config.Account{ClaudeConfigDir: "/claude"}))
	done := make(chan error, 2)
	go func() { done <- first.UpdateAccountFields("work", map[string]string{"label": "new"}) }()
	go func() { done <- second.UpdateAccountFields("work", map[string]string{"codex_home": "/codex"}) }()
	testutil.NoError(t, <-done)
	testutil.NoError(t, <-done)
	rows, err := first.Accounts()
	testutil.NoError(t, err)
	testutil.DeepEqual(t, rows["work"], config.Account{Label: "new", ClaudeConfigDir: "/claude", CodexHome: "/codex"})
}

func TestUpdateAccountFields_ConcurrentDirectoryClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sql")
	first, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	testutil.NoError(t, first.SetAccount("work", config.Account{ClaudeConfigDir: "/claude", CodexHome: "/codex"}))
	left := first.Config()
	a := left.Accounts["work"]
	a.ClaudeConfigDir = ""
	left.Accounts["work"] = a
	testutil.NoError(t, left.ValidateAccount("work"))
	right := second.Config()
	b := right.Accounts["work"]
	b.CodexHome = ""
	right.Accounts["work"] = b
	testutil.NoError(t, right.ValidateAccount("work"))
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- first.UpdateAccountFields("work", map[string]string{"claude_config_dir": ""})
	}()
	go func() { <-start; results <- second.UpdateAccountFields("work", map[string]string{"codex_home": ""}) }()
	close(start)
	accepted, rejected := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			accepted++
		} else {
			rejected++
			testutil.Contains(t, err.Error(), "define claude_config_dir and/or codex_home")
		}
	}
	testutil.Equal(t, accepted, 1)
	testutil.Equal(t, rejected, 1)
	rows, err := first.Accounts()
	testutil.NoError(t, err)
	testutil.True(t, (rows["work"].ClaudeConfigDir == "") != (rows["work"].CodexHome == ""))
	testutil.NoError(t, first.Config().ValidateAccount("work"))
}

func TestUpdateAccountFields_RejectsInvalidMergedRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  map[string]string
		message string
	}{
		{"last directories", map[string]string{"claude_config_dir": "", "codex_home": ""}, "define claude_config_dir and/or codex_home"},
		{"relative", map[string]string{"codex_home": "relative"}, "absolute or ~-prefixed"},
		{"sensitive", map[string]string{"codex_home": "~/.ssh/codex"}, "must not be inside"},
		{"tool default", map[string]string{"claude_config_dir": "~/.claude"}, "tool default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			d := testDB(t)
			original := config.Account{Label: "old", ClaudeConfigDir: "/claude", CodexHome: "/codex"}
			testutil.NoError(t, d.SetAccount("work", original))
			err := d.UpdateAccountFields("work", tc.fields)
			var invalid *AccountValidationError
			testutil.True(t, errors.As(err, &invalid))
			if invalid == nil {
				return
			}
			testutil.Contains(t, err.Error(), tc.message)
			testutil.ErrorIs(t, err, invalid.Err)
			rows, readErr := d.Accounts()
			testutil.NoError(t, readErr)
			testutil.DeepEqual(t, rows["work"], original)
			testutil.NoError(t, d.UpdateAccountFields("work", map[string]string{"label": "valid"}))
		})
	}
}

func TestUpdateAccountFields_Rollback(t *testing.T) {
	for _, phase := range []string{"write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			d := testDB(t)
			original := config.Account{Label: "old", ClaudeConfigDir: "/claude"}
			testutil.NoError(t, d.SetAccount("work", original))
			trigger := `CREATE TRIGGER reject_edit BEFORE UPDATE ON accounts BEGIN SELECT RAISE(ABORT, 'rejected edit'); END`
			if phase == "commit" {
				_, err := d.conn.Exec(`CREATE TABLE edit_parent(id TEXT PRIMARY KEY); CREATE TABLE edit_child(parent TEXT REFERENCES edit_parent(id) DEFERRABLE INITIALLY DEFERRED)`)
				testutil.NoError(t, err)
				trigger = `CREATE TRIGGER reject_edit AFTER UPDATE ON accounts BEGIN INSERT INTO edit_child(parent) VALUES ('missing'); END`
			}
			_, err := d.conn.Exec(trigger)
			testutil.NoError(t, err)
			testutil.True(t, d.UpdateAccountFields("work", map[string]string{"label": "rejected"}) != nil)
			rows, err := d.Accounts()
			testutil.NoError(t, err)
			testutil.DeepEqual(t, rows["work"], original)
			_, err = d.conn.Exec(`DROP TRIGGER reject_edit`)
			testutil.NoError(t, err)
			testutil.NoError(t, d.UpdateAccountFields("work", map[string]string{"label": "accepted"}))
		})
	}
}
