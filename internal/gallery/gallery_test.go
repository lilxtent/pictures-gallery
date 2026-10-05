package gallery

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func newGallery(t *testing.T) *Gallery {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Gallery{Store: st, Disk: &images.Disk{Root: filepath.Join(dir, "images")}}
}

func exists(t *testing.T, g *Gallery, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(g.Disk.Root, filepath.FromSlash(dir), name))
	return err == nil
}

func photo(t *testing.T) Photo {
	return Photo{Original: testutil.JPEG(t, 300, 200, color.White)}
}

func TestValidate(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if errs := (PaintingInput{Title: "Пион", Year: 2025}).Validate(now); len(errs) != 0 {
		t.Fatalf("valid input got errors %v", errs)
	}
	errs := (PaintingInput{Title: "   ", Year: 1800}).Validate(now)
	if errs["title"] != "Укажите название" {
		t.Errorf("title error = %q", errs["title"])
	}
	if errs["year"] != "Год должен быть от 1900 до 2026" {
		t.Errorf("year error = %q", errs["year"])
	}
	if errs := (PaintingInput{Title: "x", Year: 2027}).Validate(now); errs["year"] == "" {
		t.Error("future year must be rejected")
	}
}

func TestAddPaintingStoresRowAndFiles(t *testing.T) {
	g := newGallery(t)
	p, err := g.AddPainting(context.Background(), PaintingInput{
		Title: "  Пион ", Technique: "Бумага, акварель", Description: "Строка\r\nвторая\r\n", Visible: true,
	}, Photo{Original: testutil.JPEG(t, 300, 200, color.White), Crop: images.Crop{W: 100, H: 100, Rotation: -90}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Пион" || p.Description != "Строка\nвторая" || p.Slug != "pion" {
		t.Fatalf("painting = %+v", p)
	}
	if p.ImageVersion != 1 || p.Crop.Rotation != 270 {
		t.Fatalf("version = %d, rotation = %d", p.ImageVersion, p.Crop.Rotation)
	}
	dir := images.PaintingDir(p.ID)
	for _, name := range []string{"original.jpg", "v1-600.jpg", "v1-1200.jpg", "v1-2000.jpg"} {
		if !exists(t, g, dir, name) {
			t.Errorf("%s/%s missing", dir, name)
		}
	}
}

func TestAddPaintingRequiresPhoto(t *testing.T) {
	g := newGallery(t)
	if _, err := g.AddPainting(context.Background(), PaintingInput{Title: "x"}, Photo{}); !errors.Is(err, ErrNoPhoto) {
		t.Fatalf("err = %v, want ErrNoPhoto", err)
	}
}

func TestAddPaintingWithBadPhotoLeavesNothing(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	_, err := g.AddPainting(ctx, PaintingInput{Title: "x"}, Photo{Original: []byte("nope")})
	if !errors.Is(err, images.ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
	if all, _ := g.Store.ListPaintings(ctx, false); len(all) != 0 {
		t.Fatalf("no painting should be stored, got %d", len(all))
	}
}

func TestUpdateTextOnlyKeepsImage(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион", Visible: true}, photo(t))
	got, err := g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Розовый пион", Year: 2025}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Розовый пион" || got.Year != 2025 || got.Visible || got.ImageVersion != 1 || got.Slug != "pion" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateRecropReusesOriginalAndBumpsVersion(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион"}, photo(t))
	got, err := g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Пион"}, &Photo{Crop: images.Crop{X: 0, Y: 0, W: 100, H: 50}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageVersion != 2 || got.ImageWidth != 100 || got.ImageHeight != 50 {
		t.Fatalf("got %+v", got)
	}
	dir := images.PaintingDir(p.ID)
	if exists(t, g, dir, "v1-600.jpg") || !exists(t, g, dir, "v2-600.jpg") || !exists(t, g, dir, "original.jpg") {
		t.Fatal("expected v2 variants, no v1 variants, original kept")
	}
}

func TestUpdateReplacesOriginal(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион"}, photo(t))
	got, err := g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Пион"}, &Photo{Original: testutil.PNG(t, 40, 30, color.Black)})
	if err != nil {
		t.Fatal(err)
	}
	dir := images.PaintingDir(p.ID)
	if got.ImageWidth != 40 || !exists(t, g, dir, "original.png") || exists(t, g, dir, "original.jpg") {
		t.Fatalf("original not replaced: %+v", got)
	}
}

func TestUpdateUnknownPainting(t *testing.T) {
	g := newGallery(t)
	if _, err := g.UpdatePainting(context.Background(), 99, PaintingInput{Title: "x"}, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletePaintingRemovesFiles(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион"}, photo(t))
	if err := g.DeletePainting(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(g.Disk.Root, "paintings", "1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("painting dir should be removed")
	}
	if _, err := g.Store.GetPainting(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("row should be removed")
	}
}

func TestSetAboutPhoto(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	if err := g.SetAboutPhoto(ctx, photo(t)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetAboutPhoto(ctx, Photo{Crop: images.Crop{W: 50, H: 50}}); err != nil { // re-crop
		t.Fatal(err)
	}
	info, _ := site.Load(ctx, g.Store)
	if info.AboutPhoto == nil || info.AboutPhoto.Version != 2 || info.AboutPhoto.Width != 50 {
		t.Fatalf("AboutPhoto = %+v", info.AboutPhoto)
	}
	if exists(t, g, images.AboutDir, "v1-600.jpg") || !exists(t, g, images.AboutDir, "v2-600.jpg") {
		t.Fatal("expected only v2 variants")
	}
}

func TestSetAboutPhotoRecropWithoutOriginalFails(t *testing.T) {
	g := newGallery(t)
	if err := g.SetAboutPhoto(context.Background(), Photo{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}

func TestAddAndUpdatePaintingKeepCategory(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	land := store.Category{Name: "Пейзажи"}
	if err := g.Store.CreateCategory(ctx, &land); err != nil {
		t.Fatal(err)
	}
	p, err := g.AddPainting(ctx, PaintingInput{Title: "Река", Visible: true, CategoryID: &land.ID}, photo(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.CategoryID == nil || *p.CategoryID != land.ID {
		t.Fatalf("added painting category = %v, want %d", p.CategoryID, land.ID)
	}
	p, err = g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Река", Visible: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.CategoryID != nil {
		t.Fatalf("category must be cleared by an update without one, got %d", *p.CategoryID)
	}
}
