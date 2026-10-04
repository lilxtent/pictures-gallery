package web

import (
	"context"
	"image/color"
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestAboutWithTextAndPhoto(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{site.KeyAboutText: "Абзац один\n\nАбзац два"})
	if err := e.g.SetAboutPhoto(context.Background(), gallery.Photo{Original: testutil.JPEG(t, 300, 400, color.White)}); err != nil {
		t.Fatal(err)
	}
	rec := e.get("/about")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"<p>Абзац один</p>", "<p>Абзац два</p>", "/media/about/v1-1200.jpg", `class="cur" aria-current="page">Об авторе`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "text-only") {
		t.Error("page with photo must not use the text-only layout")
	}
}

func TestAboutEmpty(t *testing.T) {
	body := newEnv(t).get("/about").Body.String()
	if !strings.Contains(body, "Скоро здесь появится рассказ об авторе") || !strings.Contains(body, "text-only") {
		t.Errorf("unexpected body:\n%s", body)
	}
}

func TestContacts(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{site.KeyTelegram: "@anna_art", site.KeyEmail: "anna@example.ru"})
	body := e.get("/contacts").Body.String()
	for _, want := range []string{"Telegram", `href="https://t.me/anna_art"`, "@anna_art", `href="mailto:anna@example.ru"`, "Почта"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Error("a contact link was rejected by html/template")
	}
}

func TestContactsEmpty(t *testing.T) {
	if body := newEnv(t).get("/contacts").Body.String(); !strings.Contains(body, "Контакты скоро появятся") {
		t.Error("empty contacts text missing")
	}
}

func TestSitemap(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	e.add(gallery.PaintingInput{Title: "Скрытая", Visible: false})
	rec := e.get("/sitemap.xml")
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/xml") {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<loc>https://example.ru/</loc>", "<loc>https://example.ru/about</loc>",
		"<loc>https://example.ru/contacts</loc>", "<loc>https://example.ru/paintings/pion</loc>", "<lastmod>"} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap should contain %q", body)
		}
	}
	if strings.Contains(body, "skrytaya") {
		t.Error("hidden painting must not be in the sitemap")
	}
}

func TestRobots(t *testing.T) {
	body := newEnv(t).get("/robots.txt").Body.String()
	if !strings.Contains(body, "Disallow: /admin") || !strings.Contains(body, "Sitemap: https://example.ru/sitemap.xml") {
		t.Errorf("robots.txt = %q", body)
	}
}
