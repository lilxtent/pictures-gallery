package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

func TestHomeListsVisiblePaintingsInOrder(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Карпы кои", Technique: "Бумага, акварель", Visible: true})
	e.add(gallery.PaintingInput{Title: "Скрытая работа", Visible: false})
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})

	rec := e.get("/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(body, "Скрытая работа") {
		t.Error("hidden painting must not be listed")
	}
	iPeony, iKoi := strings.Index(body, "Пион"), strings.Index(body, "Карпы кои")
	if iPeony < 0 || iKoi < 0 || iPeony > iKoi {
		t.Errorf("want Пион (newest) before Карпы кои; got indexes %d, %d", iPeony, iKoi)
	}
	for _, want := range []string{`href="/paintings/pion"`, "Бумага, акварель", `/media/paintings/1/v1-600.jpg`, `loading="lazy"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestHomeEmptyState(t *testing.T) {
	body := newEnv(t).get("/").Body.String()
	if !strings.Contains(body, "Скоро здесь появятся работы") {
		t.Error("empty state text missing")
	}
	if !strings.Contains(body, site.DefaultArtistName) {
		t.Error("default artist name missing")
	}
}

func TestHomeHeaderGreetingAndFooter(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{
		site.KeyArtistName: "Анна Иванова", site.KeySubtitle: "художник, акварель",
		site.KeyGreeting: "Здравствуйте! Я пишу акварелью.", site.KeyPhone: "+7 900 000-00-00",
	})
	body := e.get("/").Body.String()
	for _, want := range []string{"Анна Иванова", "художник, акварель", "Здравствуйте! Я пишу акварелью.",
		// html/template writes "+" in attributes as "&#43;", which browsers decode back.
		"Подробнее обо мне", `href="tel:&#43;79000000000"`, `<html lang="ru">`, `<title>Анна Иванова — художник, акварель</title>`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestMediaServesVariants(t *testing.T) {
	e := newEnv(t)
	p := e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	rec := e.get(images.URL(images.PaintingDir(p.ID), 1, 600))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestMediaRejectsOriginalsAndMissingFiles(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	for _, path := range []string{
		"/media/paintings/1/original.jpg",
		"/media/paintings/1/v9-600.jpg",
		"/media/paintings/abc/v1-600.jpg",
		"/media/about/v1-600.jpg",
	} {
		rec := e.get(path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
		if strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s: 404 must not be cached forever", path)
		}
	}
}

func TestUnknownPathShowsRussian404(t *testing.T) {
	rec := newEnv(t).get("/no-such-page")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Страница не найдена") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestStaticFiles(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/static/site.css")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("site.css: status = %d, type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := e.get("/static/"); rec.Code != http.StatusNotFound {
		t.Errorf("directory listing: status = %d, want 404", rec.Code)
	}
}
