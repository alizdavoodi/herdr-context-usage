package opencodeshared

import (
	"context"
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

func TestOpenReadOnlyRefusesToWrite(t *testing.T) {
	t.Parallel()

	path := fixture(t, `CREATE TABLE session (id TEXT PRIMARY KEY)`, `CREATE TABLE message (id TEXT PRIMARY KEY)`)
	database, err := OpenReadOnly(context.Background(), path, "session", "message")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(context.Background(), `INSERT INTO session (id) VALUES ('write')`); err == nil {
		t.Fatal("a store opened for reading accepted a write into a live agent's database")
	}
}

func TestDataPathFollowsOpenCodesOwnLayout(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "elsewhere.db")
	if got, err := DataPath(explicit, "opencode.db"); err != nil || got != explicit {
		t.Fatalf("DataPath(explicit) = %q, %v; want the explicit path", got, err)
	}

	t.Setenv("XDG_DATA_HOME", "/xdg-data")
	for _, filename := range []string{"opencode.db", "opencode2.db"} {
		got, err := DataPath("", filename)
		if want := filepath.Join("/xdg-data", "opencode", filename); err != nil || got != want {
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
