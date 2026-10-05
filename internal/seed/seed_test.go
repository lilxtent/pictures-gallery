package seed

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

func TestRunSeedsOnceInOrder(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := &gallery.Gallery{Store: st, Disk: &images.Disk{Root: filepath.Join(dir, "images")}}
	ctx := context.Background()

	if err := Run(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, g); err != nil { // second run must be a no-op
		t.Fatal(err)
	}
	all, _ := st.ListPaintings(ctx, false)
	if len(all) != 3 || all[0].Title != "Карпы кои" || all[1].Title != "Жёлтая лилия" || all[2].Title != "Пион" {
		t.Fatalf("paintings = %+v", all)
	}
	if all[2].ImageWidth != 1080 || all[2].ImageHeight != 1640 {
		t.Errorf("peony should be cropped to 1080x1640, got %dx%d", all[2].ImageWidth, all[2].ImageHeight)
	}
	cats, _ := st.ListPublicCategories(ctx)
	if len(cats) != 2 || cats[0].Name != "Цветы" || cats[0].Count != 2 || cats[1].Name != "Вода" || cats[1].Count != 1 {
		t.Errorf("categories = %+v", cats)
	}
	info, _ := site.Load(ctx, st)
	if info.ArtistName != "Имя Фамилия" {
		t.Errorf("ArtistName = %q", info.ArtistName)
	}
}
