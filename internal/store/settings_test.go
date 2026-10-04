package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if v, err := s.Setting(ctx, "artist_name"); err != nil || v != "" {
		t.Fatalf("unset setting = %q, %v", v, err)
	}
	if err := s.SetSettings(ctx, map[string]string{"artist_name": "Анна", "phone": "+7 900"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(ctx, map[string]string{"phone": "+7 901"}); err != nil {
		t.Fatal(err)
	}
	all, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all["artist_name"] != "Анна" || all["phone"] != "+7 901" || len(all) != 2 {
		t.Fatalf("settings = %v", all)
	}
	if v, _ := s.Setting(ctx, "phone"); v != "+7 901" {
		t.Fatalf("phone = %q", v)
	}
}

func TestSessions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := s.CreateSession(ctx, "h1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "h2", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SessionValid(ctx, "h1", now); !ok {
		t.Error("h1 should be valid")
	}
	if ok, _ := s.SessionValid(ctx, "h2", now); ok {
		t.Error("h2 is expired")
	}
	if ok, _ := s.SessionValid(ctx, "missing", now); ok {
		t.Error("missing session must be invalid")
	}
	if err := s.DeleteExpiredSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 1 {
		t.Fatalf("sessions left = %d, want 1", n)
	}
	if err := s.DeleteSession(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SessionValid(ctx, "h1", now); ok {
		t.Error("h1 was deleted")
	}
}

func TestBackup(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	create(t, s, "Пион", true)
	dest := filepath.Join(t.TempDir(), "backup", "gallery.db")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	// Add a second painting and verify the backup is replaced atomically.
	create(t, s, "Роза", true)
	// A second backup must overwrite the first.
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	all, err := b.ListPaintings(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Title != "Роза" || all[1].Title != "Пион" {
		t.Fatalf("backup contents = %v", titles(all))
	}
}
