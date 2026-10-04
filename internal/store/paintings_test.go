package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/images"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func create(t *testing.T, s *Store, title string, visible bool) Painting {
	t.Helper()
	p := Painting{Title: title, Visible: visible, ImageVersion: 1, ImageWidth: 300, ImageHeight: 200}
	if err := s.CreatePainting(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func titles(ps []Painting) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Title
	}
	return out
}

func TestCreateAssignsIDSlugAndTopPosition(t *testing.T) {
	s := openTest(t)
	a := create(t, s, "Карпы кои", true)
	b := create(t, s, "Пион", true)
	if a.ID == 0 || a.Slug != "karpy-koi" || b.Slug != "pion" {
		t.Fatalf("got a=%+v b=%+v", a, b)
	}
	if b.Position >= a.Position {
		t.Fatalf("newer painting must be on top: a.Position=%d b.Position=%d", a.Position, b.Position)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be set")
	}
}

func TestSlugCollisionsGetSuffix(t *testing.T) {
	s := openTest(t)
	create(t, s, "Пион", true)
	second := create(t, s, "Пион", true)
	third := create(t, s, "пион!", true)
	if second.Slug != "pion-2" || third.Slug != "pion-3" {
		t.Fatalf("slugs = %q, %q", second.Slug, third.Slug)
	}
}

func TestEmptySlugFallsBack(t *testing.T) {
	s := openTest(t)
	a := create(t, s, "!!!", true)
	b := create(t, s, "???", true)
	if a.Slug != "kartina" || b.Slug != "kartina-2" {
		t.Fatalf("slugs = %q, %q", a.Slug, b.Slug)
	}
}

func TestGetPaintingRoundTrip(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := Painting{
		Title: "Пион", Technique: "Бумага, акварель", Size: "30×40 см", Year: 2025,
		Description: "Абзац", Visible: true,
		Crop:         images.Crop{X: 1, Y: 2, W: 3, H: 4, Rotation: 90},
		ImageVersion: 2, ImageWidth: 300, ImageHeight: 400,
	}
	if err := s.CreatePainting(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPainting(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != p.Title || got.Technique != p.Technique || got.Size != p.Size || got.Year != 2025 ||
		got.Description != p.Description || !got.Visible || got.Crop != p.Crop ||
		got.ImageVersion != 2 || got.ImageWidth != 300 || got.ImageHeight != 400 {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, p)
	}
	bySlug, err := s.GetPaintingBySlug(ctx, "pion")
	if err != nil || bySlug.ID != p.ID {
		t.Fatalf("GetPaintingBySlug = %+v, %v", bySlug, err)
	}
}

func TestYearZeroMeansNotSpecified(t *testing.T) {
	s := openTest(t)
	p := create(t, s, "Без года", true)
	got, _ := s.GetPainting(context.Background(), p.ID)
	if got.Year != 0 {
		t.Fatalf("Year = %d, want 0", got.Year)
	}
	var isNull bool
	s.db.QueryRow("SELECT year IS NULL FROM paintings WHERE id = ?", p.ID).Scan(&isNull)
	if !isNull {
		t.Fatal("year 0 must be stored as NULL")
	}
}

func TestNotFound(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.GetPainting(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPainting err = %v", err)
	}
	if _, err := s.GetPaintingBySlug(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPaintingBySlug err = %v", err)
	}
	if err := s.UpdatePainting(ctx, &Painting{ID: 42, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdatePainting err = %v", err)
	}
	if err := s.DeletePainting(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeletePainting err = %v", err)
	}
}

func TestUpdateKeepsSlugAndPosition(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := create(t, s, "Пион", true)
	p.Title = "Розовый пион"
	p.Visible = false
	p.ImageVersion = 3
	if err := s.UpdatePainting(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPainting(ctx, p.ID)
	if got.Title != "Розовый пион" || got.Slug != "pion" || got.Visible || got.ImageVersion != 3 || got.Position != p.Position {
		t.Fatalf("got %+v", got)
	}
}

func TestListFiltersAndOrders(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	create(t, s, "A", true)
	create(t, s, "B", false)
	create(t, s, "C", true)
	all, _ := s.ListPaintings(ctx, false)
	if got := titles(all); len(got) != 3 || got[0] != "C" || got[1] != "B" || got[2] != "A" {
		t.Fatalf("all = %v, want [C B A]", got)
	}
	visible, _ := s.ListPaintings(ctx, true)
	if got := titles(visible); len(got) != 2 || got[0] != "C" || got[1] != "A" {
		t.Fatalf("visible = %v, want [C A]", got)
	}
}

func TestReorder(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a := create(t, s, "A", true)
	b := create(t, s, "B", true)
	c := create(t, s, "C", true)
	if err := s.ReorderPaintings(ctx, []int64{a.ID, c.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListPaintings(ctx, false)
	if got := titles(all); got[0] != "A" || got[1] != "C" || got[2] != "B" {
		t.Fatalf("order = %v, want [A C B]", got)
	}
}

func TestDelete(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := create(t, s, "A", true)
	if err := s.DeletePainting(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPainting(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestIDsAreNeverReusedAfterDelete(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	create(t, s, "A", true)
	b := create(t, s, "B", true)
	if err := s.DeletePainting(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	c := create(t, s, "C", true)
	if c.ID == b.ID {
		t.Fatalf("id %d was reused after delete", c.ID)
	}
}

func TestReopenKeepsDataAndMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	create(t, s, "A", true)
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	all, _ := s.ListPaintings(context.Background(), false)
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
}
