package admin

import (
	"context"
	"errors"
	"image/color"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestEditFormShowsValues(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p, _ := h.g.AddPainting(ctx, gallery.PaintingInput{Title: "Пион", Technique: "Бумага, акварель", Year: 2025, Visible: true},
		gallery.Photo{Original: testutil.JPEG(t, 300, 200, color.White), Crop: images.Crop{W: 100, H: 80}})
	c, _ := h.login()
	body := h.get("/admin/paintings/1", c).Body.String()
	for _, want := range []string{`value="Пион"`, `value="Бумага, акварель"`, `value="2025"`, "Заменить фото",
		"Изменить кадрирование", `data-original="/admin/paintings/1/original"`, "width", "Удалить картину",
		`href="/paintings/` + p.Slug + `"`, images.URL(images.PaintingDir(p.ID), 1, 600)} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "data-required") {
		t.Error("editing must not require a new photo")
	}
}

func TestEditUnknownOrBadID(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	for _, path := range []string{"/admin/paintings/99", "/admin/paintings/abc"} {
		if rec := h.get(path, c); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d", path, rec.Code)
		}
	}
	if rec := h.postForm("/admin/paintings/99", url.Values{"csrf": {csrf}, "title": {"x"}}, c); rec.Code != http.StatusNotFound {
		t.Errorf("POST unknown: status = %d", rec.Code)
	}
}

func TestUpdateTextOnly(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Розовый пион", "year": "2024"}, nil), c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings?msg=saved" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	p, _ := h.st.GetPainting(context.Background(), 1)
	if p.Title != "Розовый пион" || p.Year != 2024 || p.Visible || p.ImageVersion != 1 {
		t.Fatalf("painting = %+v", p)
	}
}

func TestUpdateRecrop(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{
		"csrf": csrf, "title": "Пион", "visible": "on",
		"crop_changed": "1", "crop_x": "10", "crop_y": "10", "crop_w": "50", "crop_h": "40", "crop_rotate": "0",
	}, nil), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	p, _ := h.st.GetPainting(context.Background(), 1)
	if p.ImageVersion != 2 || p.ImageWidth != 50 || p.ImageHeight != 40 {
		t.Fatalf("painting = %+v", p)
	}
}

func TestUpdateReplacesPhoto(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Пион"},
		testutil.PNG(t, 40, 30, color.Black)), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(h.g.Disk.Root, "paintings", "1", "original.png")); err != nil {
		t.Fatal("new original should be stored")
	}
}

func TestUpdateValidation(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": ""}, nil), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Укажите название") {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<h1>Пион</h1>") {
		t.Error("the page heading should keep the saved title")
	}
}

func TestOriginalIsPrivate(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	if rec := h.get("/admin/paintings/1/original", nil); location(rec) != "/admin/login" {
		t.Fatalf("logged out: location = %q", location(rec))
	}
	c, _ := h.login()
	rec := h.get("/admin/paintings/1/original", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d, headers = %v", rec.Code, rec.Header())
	}
}

func TestDeleteFlow(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()

	body := h.get("/admin/paintings/1/delete", c).Body.String()
	if !strings.Contains(body, "Удалить картину?") || !strings.Contains(body, "«Пион»") {
		t.Fatalf("confirm page = %s", body)
	}
	if rec := h.postForm("/admin/paintings/1/delete", url.Values{}, c); rec.Code != http.StatusForbidden {
		t.Fatalf("without csrf: status = %d", rec.Code)
	}
	rec := h.postForm("/admin/paintings/1/delete", url.Values{"csrf": {csrf}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings?msg=deleted" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	if _, err := h.st.GetPainting(context.Background(), 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("painting should be deleted")
	}
	if body := h.get("/admin/paintings?msg=deleted", c).Body.String(); !strings.Contains(body, "Картина удалена") {
		t.Error("flash message missing")
	}
}
