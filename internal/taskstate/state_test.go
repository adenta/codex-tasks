package taskstate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCommittedWALAndRejectWrites(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "state_5.sqlite")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE threads(id TEXT); INSERT INTO threads VALUES ('preserved');`); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var id string
	if err = reader.QueryRow("SELECT id FROM threads").Scan(&id); err != nil || id != "preserved" {
		t.Fatal(id, err)
	}
	if _, err = reader.Exec("DELETE FROM threads"); err == nil {
		t.Fatal("read-only connection accepted write")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("index changed", err)
	}
}
func TestCanonicalContainment(t *testing.T) {
	home := t.TempDir()
	if _, err := OpenReadOnly(context.Background(), home); err == nil {
		t.Fatal("missing DB accepted")
	}
	if _, err := os.Stat(filepath.Join(home, "state_5.sqlite")); !os.IsNotExist(err) {
		t.Fatal("missing DB created")
	}
	outside := filepath.Join(t.TempDir(), "state_5.sqlite")
	if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "state_5.sqlite")); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalStateDatabase(home); err == nil {
		t.Fatal("path escaped")
	}
	if err := os.Remove(filepath.Join(home, "state_5.sqlite")); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(home, "index")
	if err := os.WriteFile(inside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(home, "state_5.sqlite")); err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalStateDatabase(home); err != nil || got != filepath.Join(home, "state_5.sqlite") {
		t.Fatal(got, err)
	}
}
