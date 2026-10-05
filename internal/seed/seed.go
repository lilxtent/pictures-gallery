// Package seed fills an empty database with sample content for local development.
package seed

import (
	"context"
	"embed"
	"fmt"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

//go:embed photos/*.jpg
var photos embed.FS

const sampleText = "Это пример описания. Здесь будет рассказ о картине: как появилась идея, что хотелось передать, какие материалы использованы.\n\nПустая строка начинает новый абзац."

type sample struct {
	file     string
	in       gallery.PaintingInput
	crop     images.Crop
	category string // name of one of sampleCategories
}

// sampleCategories are in display order.
var sampleCategories = []string{"Цветы", "Вода"}

// samples are in display order; the crops cut away the table, pen and camera stamp.
var samples = []sample{
	{"koi.jpg", gallery.PaintingInput{Title: "Карпы кои", Technique: "Бумага, акварель", Description: sampleText, Visible: true}, images.Crop{}, "Вода"},
	{"lily.jpg", gallery.PaintingInput{Title: "Жёлтая лилия", Technique: "Бумага, акварель", Description: sampleText, Visible: true}, images.Crop{X: 0, Y: 0, W: 1330, H: 1920}, "Цветы"},
	{"peony.jpg", gallery.PaintingInput{Title: "Пион", Technique: "Бумага, акварель", Year: 2025, Description: sampleText, Visible: true}, images.Crop{X: 0, Y: 60, W: 1080, H: 1640}, "Цветы"},
}

// Run adds sample settings, categories and paintings if there are no paintings yet.
func Run(ctx context.Context, g *gallery.Gallery) error {
	existing, err := g.Store.ListPaintings(ctx, false)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if err := g.Store.SetSettings(ctx, map[string]string{
		site.KeyArtistName: "Имя Фамилия",
		site.KeySubtitle:   "художник, акварель",
		site.KeyGreeting:   "Здравствуйте! Я пишу акварелью цветы, воду и свет. Здесь собраны мои работы — каждая со своей историей.",
		site.KeyAboutText:  "Здесь будет рассказ об авторе.\n\nЕго можно изменить в разделе «Об авторе» в управлении сайтом.",
		site.KeyEmail:      "mail@example.ru",
		site.KeyTelegram:   "example",
	}); err != nil {
		return err
	}
	categoryIDs := map[string]int64{}
	for _, name := range sampleCategories {
		c := store.Category{Name: name}
		if err := g.Store.CreateCategory(ctx, &c); err != nil {
			return fmt.Errorf("seed category %s: %w", name, err)
		}
		categoryIDs[name] = c.ID
	}
	for i := len(samples) - 1; i >= 0; i-- { // each new painting goes on top
		s := samples[i]
		categoryID := categoryIDs[s.category]
		s.in.CategoryID = &categoryID
		data, err := photos.ReadFile("photos/" + s.file)
		if err != nil {
			return err
		}
		if _, err := g.AddPainting(ctx, s.in, gallery.Photo{Original: data, Crop: s.crop}); err != nil {
			return fmt.Errorf("seed %s: %w", s.file, err)
		}
	}
	return nil
}
