package admin

import (
	"bytes"
	"context"
	"image/color"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func multipartReq(t *testing.T, path string, fields map[string]string, photo []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if photo != nil {
		fw, err := mw.CreateFormFile("photo", "photo.jpg")
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(photo)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestNewFormRequiresLogin(t *testing.T) {
	if rec := newHarness(t).get("/admin/paintings/new", nil); location(rec) != "/admin/login" {
		t.Fatalf("location = %q", location(rec))
	}
}

func TestNewFormRenders(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	body := h.get("/admin/paintings/new", c).Body.String()
	for _, want := range []string{"Новая картина", `name="title"`, "data-crop", "data-required", "Выбрать фото",
		`name="visible" checked`, "/static/vendor/cropper.min.js", "/static/vendor/cropper.min.css", "data-dirty-check"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestCreatePainting(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := multipartReq(t, "/admin/paintings/new", map[string]string{
		"csrf": csrf, "title": "Пион", "technique": "Бумага, акварель", "size": "20×30 см", "year": "2025",
		"description": "Абзац", "visible": "on", "crop_x": "0", "crop_y": "0", "crop_w": "100", "crop_h": "100",
		"crop_rotate": "0", "crop_changed": "1",
	}, testutil.JPEG(t, 300, 200, color.White))
	rec := h.do(req, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings?msg=saved" {
		t.Fatalf("status = %d, location = %q, body = %s", rec.Code, location(rec), rec.Body)
	}
	all, _ := h.st.ListPaintings(context.Background(), false)
	if len(all) != 1 {
		t.Fatalf("paintings = %d", len(all))
	}
	p := all[0]
	if p.Title != "Пион" || p.Year != 2025 || !p.Visible || p.ImageWidth != 100 || p.ImageHeight != 100 {
		t.Fatalf("painting = %+v", p)
	}
}

func TestCreateValidationKeepsInput(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := multipartReq(t, "/admin/paintings/new", map[string]string{
		"csrf": csrf, "title": " ", "technique": "Холст, масло", "year": "abc",
	}, testutil.JPEG(t, 30, 20, color.White))
	rec := h.do(req, c)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Укажите название", "Год должен быть числом", "После ошибки фото нужно выбрать ещё раз",
		`value="Холст, масло"`, `value="abc"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if all, _ := h.st.ListPaintings(context.Background(), false); len(all) != 0 {
		t.Fatal("nothing must be stored")
	}
}

func TestCreateRequiresPhoto(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/new", map[string]string{"csrf": csrf, "title": "Пион"}, nil), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Выберите фото") {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCreateRejectsUnreadablePhoto(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/new", map[string]string{"csrf": csrf, "title": "Пион"}, []byte("not an image")), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Не удалось прочитать фото") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestCreateRequiresCSRF(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/new", map[string]string{"title": "Пион"}, testutil.JPEG(t, 10, 10, color.White)), c)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCropJSON(t *testing.T) {
	if got := cropJSON(images.Crop{}); got != "" {
		t.Errorf("empty crop: got %q", got)
	}
	got := cropJSON(images.Crop{X: 1, Y: 2, W: 3, H: 4, Rotation: 90})
	if got != `{"height":4,"rotate":90,"width":3,"x":1,"y":2}` {
		t.Errorf("got %q", got)
	}
}
