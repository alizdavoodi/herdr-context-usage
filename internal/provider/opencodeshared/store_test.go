package opencodeshared

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenReadOnlySeparatesLifecycleStatesFromReadFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    func(*testing.T) string
		wantErr error
		// A transient failure has no sentinel: it is whatever the filesystem or
		// the driver said, and the point is only that it is neither of ours.
		wantTransient bool
	}{
		{
			name:    "store not written yet",
			path:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "opencode.db") },
			wantErr: ErrMissing,
		},
		{
			name: "store created but not initialised",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "opencode.db")
				database, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if err := database.Ping(); err != nil {
					t.Fatal(err)
				}
				if err := database.Close(); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr: ErrSchema,
		},
		{
			name: "store has only some of the tables",
			path: func(t *testing.T) string {
				return fixture(t, `CREATE TABLE session (id TEXT PRIMARY KEY)`)
			},
			wantErr: ErrSchema,
		},
		{
			// Not a lifecycle state: we cannot tell what is going on, so the
			// displayed value survives.
			name:          "a directory where the database should be",
			path:          func(t *testing.T) string { return t.TempDir() },
			wantTransient: true,
		},
		{
			name: "unreadable store",
			path: func(t *testing.T) string {
				path := fixture(t, `CREATE TABLE session (id TEXT PRIMARY KEY)`, `CREATE TABLE message (id TEXT PRIMARY KEY)`)
				if err := os.Chmod(path, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(path, 0o600) })
				return path
			},
			wantTransient: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			database, err := OpenReadOnly(context.Background(), tt.path(t), "session", "message")
			if database != nil {
				database.Close()
			}
			if tt.wantTransient {
				if err == nil || errors.Is(err, ErrMissing) || errors.Is(err, ErrSchema) {
					t.Fatalf("OpenReadOnly() error = %v, want a transient failure", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("OpenReadOnly() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// These stores belong to running agents, so the read-only guarantee is the one
// property of this package that cannot be allowed to regress quietly. Two DSN
// parameters carry it and they are not equals: mode=ro is what refuses the
// writes, and query_only(1) is a second latch that a connection can turn off
// by itself. Both are pinned, and the difference between them is pinned too.
func TestOpenReadOnlyRefusesEveryWrite(t *testing.T) {
	t.Parallel()

	path := fixture(t, `CREATE TABLE session (id TEXT PRIMARY KEY)`, `CREATE TABLE message (id TEXT PRIMARY KEY)`)
	before := checksum(t, path)
	database, err := OpenReadOnly(context.Background(), path, "session", "message")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()

	var queryOnly int
	if err := database.QueryRowContext(ctx, `PRAGMA query_only`).Scan(&queryOnly); err != nil {
		t.Fatalf("read query_only: %v", err)
	}
	if queryOnly != 1 {
		t.Fatalf("query_only = %d on a freshly opened store, want 1", queryOnly)
	}

	writes := []string{
		`INSERT INTO session (id) VALUES ('write')`,
		`UPDATE session SET id = 'write'`,
		`DELETE FROM session`,
		`CREATE TABLE injected (id TEXT)`,
		`DROP TABLE session`,
	}
	for _, write := range writes {
		if _, err := database.ExecContext(ctx, write); err == nil {
			t.Fatalf("a store opened for reading accepted %q against a live agent's database", write)
		}
	}

	// query_only is advisory: the connection can clear it. Every write above
	// must still be refused afterwards, which is what proves mode=ro is the half
	// carrying the guarantee rather than a belt beside a brace.
	if _, err := database.ExecContext(ctx, `PRAGMA query_only = 0`); err != nil {
		t.Fatalf("clearing query_only: %v", err)
	}
	for _, write := range writes {
		if _, err := database.ExecContext(ctx, write); err == nil {
			t.Fatalf("with query_only cleared, %q was accepted; mode=ro is no longer refusing writes", write)
		}
	}

	if after := checksum(t, path); after != before {
		t.Fatal("the database file changed while it was open for reading")
	}
}

func checksum(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(contents)
}

func TestDataPathFollowsOpenCodesOwnLayout(t *testing.T) {
	t.Parallel()

	dataHome := t.TempDir()
	explicit := filepath.Join(t.TempDir(), "elsewhere.db")
	if got, err := DataPath(explicit, dataHome, "opencode.db"); err != nil || got != explicit {
		t.Fatalf("DataPath(explicit) = %q, %v; want the explicit path", got, err)
	}

	for _, filename := range []string{"opencode.db", "opencode2.db"} {
		got, err := DataPath("", dataHome, filename)
		if want := filepath.Join(dataHome, "opencode", filename); err != nil || got != want {
			t.Fatalf("DataPath(%q) = %q, %v; want %q", filename, got, err, want)
		}
	}
}

func fixture(t *testing.T, statements ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
