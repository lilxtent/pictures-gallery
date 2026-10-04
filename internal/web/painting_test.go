package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

func TestPaintingPage(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{site.KeyArtistName: "Анна"})
	p := e.add(gallery.PaintingInput{
		Title: "Карпы кои", Technique: "Бумага, акварель", Size: "21×30 см", Year: 2025, Visible: true,
		Description: "Первый абзац\nвторая строка\n\nВторой абзац <script>alert(1)</script>",
	})
	rec := e.get("/paintings/" + p.Slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<h1>Карпы кои</h1>",
		"Бумага, акварель · 21×30 см · 2025",
		"<p>Первый абзац<br>вторая строка</p>",
		"&lt;script&gt;",
		`href="/media/paintings/1/v1-2000.jpg"`,
		"data-lightbox",
		`<meta property="og:image" content="https://example.ru/media/paintings/1/v1-1200.jpg">`,
		`<meta property="og:type" content="article">`,
		"<title>Карпы кои — Анна</title>",
		`src="/static/site.js"`,
		"← Все работы",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("description must be escaped")
	}
}

func TestPaintingPageHiddenOrUnknownIs404(t *testing.T) {
	e := newEnv(t)
	hidden := e.add(gallery.PaintingInput{Title: "Скрытая", Visible: false})
	for _, path := range []string{"/paintings/" + hidden.Slug, "/paintings/net-takoy"} {
		if rec := e.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestPaintingPrevNextSkipHidden(t *testing.T) {
	e := newEnv(t)
	first := e.add(gallery.PaintingInput{Title: "Первая", Visible: true})
	e.add(gallery.PaintingInput{Title: "Скрытая", Visible: false})
	third := e.add(gallery.PaintingInput{Title: "Третья", Visible: true})
	// Site order (newest first): Третья, [Скрытая], Первая.

	top := e.get("/paintings/" + third.Slug).Body.String()
	if !strings.Contains(top, "Первая →") || strings.Contains(top, "Скрытая") {
		t.Errorf("top painting nav wrong:\n%s", top)
	}
	bottom := e.get("/paintings/" + first.Slug).Body.String()
	if !strings.Contains(bottom, "← Третья") || strings.Contains(bottom, "Скрытая") {
		t.Errorf("bottom painting nav wrong:\n%s", bottom)
	}
}
