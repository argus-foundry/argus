package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

// openFreshInMemory is the pre-template OpenInMemory: replay createTables on a
// brand new :memory: connection. It is the reference the clone must match.
func openFreshInMemory(t *testing.T) *DB {
	t.Helper()
	conn, err := sql.Open("sqlite", memDSN)
	testutil.NoError(t, err)
	conn.SetMaxOpenConns(1)
	d := &DB{conn: conn}
	testutil.NoError(t, d.createTables())
	testutil.NoError(t, d.seedDefaults())
	testutil.NoError(t, d.sweepLegacyKeybindings())
	t.Cleanup(func() { _ = conn.Close() })
	return d
}

func schemaRows(t *testing.T, d *DB) [][3]string {
	t.Helper()
	rows, err := d.conn.Query(`SELECT type, name, COALESCE(sql,'') FROM sqlite_master ORDER BY type, name`)
	testutil.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out [][3]string
	for rows.Next() {
		var r [3]string
		testutil.NoError(t, rows.Scan(&r[0], &r[1], &r[2]))
		out = append(out, r)
	}
	testutil.NoError(t, rows.Err())
	return out
}

func pragma(t *testing.T, d *DB, name string) string {
	t.Helper()
	var v string
	testutil.NoError(t, d.conn.QueryRow("PRAGMA "+name).Scan(&v))
	return v
}

func dumpTable(t *testing.T, d *DB, table string) [][]string {
	t.Helper()
	rows, err := d.conn.Query(fmt.Sprintf("SELECT * FROM %q ORDER BY 1, 2", table))
	testutil.NoError(t, err)
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out [][]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		testutil.NoError(t, rows.Scan(ptrs...))
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	return out
}

func TestOpenInMemory_CloneMatchesFreshlyMigrated(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // seeding reads HOME; pin it so both sides agree
	clone, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = clone.Close() }()
	fresh := openFreshInMemory(t)

	t.Run("schema", func(t *testing.T) {
		got, want := schemaRows(t, clone), schemaRows(t, fresh)
		if len(got) < 10 {
			t.Fatalf("suspiciously small schema: %d objects", len(got))
		}
		testutil.DeepEqual(t, got, want)
	})
	t.Run("pragmas", func(t *testing.T) {
		for _, p := range []string{"foreign_keys", "user_version", "journal_mode", "busy_timeout", "encoding", "page_size", "auto_vacuum", "synchronous"} {
			testutil.Equal(t, pragma(t, clone, p), pragma(t, fresh, p))
		}
		testutil.Equal(t, pragma(t, clone, "foreign_keys"), "1")
	})
	t.Run("foreign keys enforced", func(t *testing.T) {
		// hera cascade tests depend on FK enforcement surviving the clone.
		_, err := clone.conn.Exec(`INSERT INTO hera_roles (orchestrator_id, name, kind, argus_project, created_at) VALUES (999999, 'x', 'worker', 'p', 'now')`)
		if err == nil {
			t.Fatal("expected FK violation inserting a role with no orchestrator")
		}
	})
	t.Run("seeded rows", func(t *testing.T) {
		for _, tbl := range []string{"backends", "config", "projects", "tasks"} {
			testutil.DeepEqual(t, dumpTable(t, clone, tbl), dumpTable(t, fresh, tbl))
		}
		if len(dumpTable(t, clone, "backends")) == 0 {
			t.Fatal("expected seeded backends")
		}
	})
	t.Run("data_version advances on a write", func(t *testing.T) {
		before := mustDataVersion(t, clone)
		_, err := clone.conn.Exec(`INSERT INTO config (key, value) VALUES ('zz.k', 'v')`)
		testutil.NoError(t, err)
		// data_version only changes for commits by OTHER connections; the
		// clone must behave like a fresh DB, whatever that is.
		freshBefore := mustDataVersion(t, fresh)
		_, err = fresh.conn.Exec(`INSERT INTO config (key, value) VALUES ('zz.k', 'v')`)
		testutil.NoError(t, err)
		testutil.Equal(t, mustDataVersion(t, clone)-before, mustDataVersion(t, fresh)-freshBefore)
	})
}

func mustDataVersion(t *testing.T, d *DB) int {
	t.Helper()
	v, err := d.DataVersion()
	testutil.NoError(t, err)
	return int(v)
}

func TestOpenInMemory_SeedingRunsPerOpen(t *testing.T) {
	// Seeding depends on the environment, so it must not be frozen into the
	// template: opens under different HOMEs see their own value.
	home1, home2 := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home1)
	want1 := config.DefaultMetisVaultPath()
	a, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = a.Close() }()
	t.Setenv("HOME", home2)
	want2 := config.DefaultMetisVaultPath()
	testutil.NotEqual(t, want1, want2)
	b, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = b.Close() }()

	get := func(d *DB) string {
		var v string
		testutil.NoError(t, d.conn.QueryRow(`SELECT value FROM config WHERE key='kb.metis_vault_path'`).Scan(&v))
		return v
	}
	testutil.Equal(t, get(a), want1)
	testutil.Equal(t, get(b), want2)
}

func TestOpenInMemory_Isolation(t *testing.T) {
	a, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = a.Close() }()
	b, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = b.Close() }()

	_, err = a.conn.Exec(`INSERT INTO config (key, value) VALUES ('iso.key', 'only-in-a')`)
	testutil.NoError(t, err)

	count := func(d *DB) int {
		var n int
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM config WHERE key='iso.key'`).Scan(&n))
		return n
	}
	testutil.Equal(t, count(a), 1)
	testutil.Equal(t, count(b), 0)

	// A later clone must not see it either: the template itself is untouched.
	c, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = c.Close() }()
	testutil.Equal(t, count(c), 0)

}

func TestOpenInMemory_ParallelOpens(t *testing.T) {
	const workers, iters = 16, 8
	var wg sync.WaitGroup
	errs := make(chan error, workers*iters)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				d, err := OpenInMemory()
				if err != nil {
					errs <- err
					return
				}
				key := "par." + fmt.Sprint(w)
				if _, err := d.conn.Exec(`INSERT INTO config (key, value) VALUES (?, 'v')`, key); err != nil {
					errs <- err
				}
				var n int
				if err := d.conn.QueryRow(`SELECT COUNT(*) FROM config WHERE key LIKE 'par.%'`).Scan(&n); err != nil {
					errs <- err
				} else if n != 1 {
					errs <- errors.New("write leaked between clones")
				}
				_ = d.Close()
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestOpenInMemory_TemplateBuildFailureIsAnError(t *testing.T) {
	memTemplate.mu.Lock()
	saved, savedBuild := memTemplate.conn, memTemplate.build
	memTemplate.conn = nil
	boom := errors.New("boom")
	memTemplate.build = func() (driver.Conn, error) { return nil, boom }
	memTemplate.mu.Unlock()
	t.Cleanup(func() {
		memTemplate.mu.Lock()
		memTemplate.conn, memTemplate.build = saved, savedBuild
		memTemplate.mu.Unlock()
	})

	d, err := OpenInMemory()
	if err == nil {
		_ = d.Close()
		t.Fatal("expected an error, got a DB")
	}
	testutil.ErrorIs(t, err, boom)
	testutil.Nil(t, d)

	// The failure is not cached: once the builder works, opens succeed.
	memTemplate.mu.Lock()
	memTemplate.build = nil
	memTemplate.mu.Unlock()
	d, err = OpenInMemory()
	testutil.NoError(t, err)
	_ = d.Close()
}

// Open(path) must keep running the real migrations, independent of the
// template: an old-schema file gets upgraded and ends up matching the template.
func TestOpen_PathStillMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sql")
	old, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = old.Exec(`CREATE TABLE tasks (id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', created_at TEXT NOT NULL DEFAULT '')`)
	testutil.NoError(t, err)
	_, err = old.Exec(`INSERT INTO tasks (id, name) VALUES ('t1', 'legacy')`)
	testutil.NoError(t, err)
	testutil.NoError(t, old.Close())

	d, err := Open(path)
	testutil.NoError(t, err)
	defer func() { _ = d.Close() }()

	var name string
	testutil.NoError(t, d.conn.QueryRow(`SELECT name FROM tasks WHERE id='t1'`).Scan(&name))
	testutil.Equal(t, name, "legacy")
	// Columns added by later migrations exist on the upgraded table.
	var acct string
	testutil.NoError(t, d.conn.QueryRow(`SELECT account FROM tasks WHERE id='t1'`).Scan(&acct))

	mem, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = mem.Close() }()
	tableNames := func(x *DB) []string {
		var out []string
		for _, r := range schemaRows(t, x) {
			if r[0] == "table" {
				out = append(out, r[1])
			}
		}
		return out
	}
	if !reflect.DeepEqual(tableNames(d), tableNames(mem)) {
		t.Fatalf("migrated file tables %v != template tables %v", tableNames(d), tableNames(mem))
	}
}

func TestOpenInMemory_SecondConnectionErrorsLoudly(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = d.Close() }()

	// Hold the only connection; a query on another pooled connection must fail
	// with the explicit error instead of reading a blank or empty DB.
	held, err := d.conn.Conn(context.Background())
	testutil.NoError(t, err)
	defer func() { _ = held.Close() }()
	var n int
	err = d.conn.QueryRow(`SELECT COUNT(*) FROM config`).Scan(&n)
	testutil.ErrorIs(t, err, errMemSecondConn)

	// Open rows pin the connection too.
	testutil.NoError(t, held.Close())
	rows, err := d.conn.Query(`SELECT key FROM config`)
	testutil.NoError(t, err)
	defer func() { _ = rows.Close() }()
	err = d.conn.QueryRow(`SELECT COUNT(*) FROM config`).Scan(&n)
	testutil.ErrorIs(t, err, errMemSecondConn)
}

func TestOpenInMemory_SerialUseKeepsTheConnection(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	defer func() { _ = d.Close() }()

	// Many serial ops, a transaction, and idle gaps must all reuse the one
	// pooled connection (no reconnect, so no errMemSecondConn).
	for i := 0; i < 20; i++ {
		_, err := d.conn.Exec(`INSERT INTO config (key, value) VALUES (?, 'v')`, fmt.Sprint("serial.", i))
		testutil.NoError(t, err)
	}
	testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO config (key, value) VALUES ('serial.tx', 'v')`)
		return err
	}))
	var n int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM config WHERE key LIKE 'serial.%'`).Scan(&n))
	testutil.Equal(t, n, 21)
	testutil.Equal(t, d.conn.Stats().OpenConnections, 1)
}
