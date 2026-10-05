package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func newCategory(t *testing.T, s *Store, name string) Category {
	t.Helper()
	c := Category{Name: name}
	if err := s.CreateCategory(context.Background(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func assign(t *testing.T, s *Store, p *Painting, c Category) {
	t.Helper()
	p.CategoryID = &c.ID
	if err := s.UpdatePainting(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func categoryNames(cs []Category) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func TestCreateCategoryAssignsSlugAndAppendsToEnd(t *testing.T) {
	s := openTest(t)
	a := newCategory(t, s, "Пейзажи")
	b := newCategory(t, s, "Портреты")
	if a.ID == 0 || a.Slug != "peyzazhi" || b.Slug != "portrety" {
		t.Fatalf("got a=%+v b=%+v", a, b)
	}
	if b.Position <= a.Position {
		t.Fatalf("newer category must be last: a=%d b=%d", a.Position, b.Position)
	}
}

func TestCreateCategoryMakesSlugUnique(t *testing.T) {
	s := openTest(t)
	a := newCategory(t, s, "Море")
	b := newCategory(t, s, "море")
	if a.Slug == b.Slug {
		t.Fatalf("slugs must differ, both %q", a.Slug)
	}
}

func TestRenameCategoryKeepsSlug(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	c := newCategory(t, s, "Море")
	if err := s.RenameCategory(ctx, c.ID, "Океан"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCategory(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Океан" || got.Slug != c.Slug {
		t.Fatalf("got %+v", got)
	}
	if err := s.RenameCategory(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestGetCategoryBySlugNotFound(t *testing.T) {
	s := openTest(t)
	if _, err := s.GetCategoryBySlug(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestReorderCategories(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a := newCategory(t, s, "A")
	b := newCategory(t, s, "B")
	c := newCategory(t, s, "C")
	if err := s.ReorderCategories(ctx, []int64{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if names := categoryNames(got); len(names) != 3 || names[0] != "C" || names[1] != "A" || names[2] != "B" {
		t.Fatalf("got %v", names)
	}
}

func TestPaintingKeepsCategoryAcrossSaveAndLoad(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	c := newCategory(t, s, "Пейзажи")
	p := create(t, s, "Река", true)
	if p.CategoryID != nil {
		t.Fatal("new painting must be uncategorised")
	}
	assign(t, s, &p, c)
	got, err := s.GetPainting(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID == nil || *got.CategoryID != c.ID {
		t.Fatalf("got %v, want %d", got.CategoryID, c.ID)
	}
	got.CategoryID = nil
	if err := s.UpdatePainting(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetPainting(ctx, p.ID); got.CategoryID != nil {
		t.Fatalf("category must be cleared, got %d", *got.CategoryID)
	}
}

func TestCreatePaintingRejectsUnknownCategory(t *testing.T) {
	s := openTest(t)
	missing := int64(999)
	p := Painting{Title: "Река", CategoryID: &missing, ImageVersion: 1}
	if err := s.CreatePainting(context.Background(), &p); err == nil {
		t.Fatal("expected a foreign key error")
	}
}

func TestDeleteCategoryUncategorisesPaintings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	c := newCategory(t, s, "Пейзажи")
	p := create(t, s, "Река", true)
	assign(t, s, &p, c)
	if err := s.DeleteCategory(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPainting(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID != nil {
		t.Fatalf("painting must be uncategorised, got %d", *got.CategoryID)
	}
	if err := s.DeleteCategory(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestListCategoriesCountsAllPaintings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	c := newCategory(t, s, "Пейзажи")
	newCategory(t, s, "Пустая")
	a := create(t, s, "Река", true)
	b := create(t, s, "Лес", false)
	assign(t, s, &a, c)
	assign(t, s, &b, c)
	got, err := s.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Count != 2 || got[1].Count != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestListPublicCategoriesSkipsOnesWithoutVisiblePaintings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	shown := newCategory(t, s, "Пейзажи")
	hidden := newCategory(t, s, "Скрытая")
	newCategory(t, s, "Пустая")
	a := create(t, s, "Река", true)
	b := create(t, s, "Лес", true)
	c := create(t, s, "Тайна", false)
	assign(t, s, &a, shown)
	assign(t, s, &b, shown)
	assign(t, s, &c, hidden)
	got, err := s.ListPublicCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != shown.ID || got[0].Count != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestListVisiblePaintingsInCategory(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	c := newCategory(t, s, "Пейзажи")
	other := newCategory(t, s, "Портреты")
	a := create(t, s, "Река", true)
	b := create(t, s, "Лес", true)
	hidden := create(t, s, "Тайна", false)
	elsewhere := create(t, s, "Дама", true)
	create(t, s, "Без категории", true)
	assign(t, s, &a, c)
	assign(t, s, &b, c)
	assign(t, s, &hidden, c)
	assign(t, s, &elsewhere, other)
	got, err := s.ListVisiblePaintingsInCategory(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// b was created after a, so it is on top.
	if names := titles(got); len(names) != 2 || names[0] != "Лес" || names[1] != "Река" {
		t.Fatalf("got %v", names)
	}
}

func TestMigrationAddsCategoriesToExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		string(body),
		`INSERT INTO paintings (slug, title, position, created_at, updated_at) VALUES ('reka', 'Река', 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		"PRAGMA user_version = 1",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetPaintingBySlug(context.Background(), "reka")
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID != nil {
		t.Fatalf("existing painting must stay uncategorised, got %d", *got.CategoryID)
	}
}
