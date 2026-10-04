package admin

import (
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func (h *harness) addPainting(title string, visible bool) store.Painting {
	h.t.Helper()
	p, err := h.g.AddPainting(context.Background(), gallery.PaintingInput{Title: title, Visible: visible},
		gallery.Photo{Original: testutil.JPEG(h.t, 300, 200, color.White)})
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func TestListRequiresLogin(t *testing.T) {
	if rec := newHarness(t).get("/admin/paintings", nil); location(rec) != "/admin/login" {
		t.Fatalf("location = %q", location(rec))
	}
}

func TestListShowsPaintings(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	h.addPainting("Скрытая", false)
	c, csrf := h.login()
	rec := h.get("/admin/paintings?msg=saved", c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Пион", "Скрытая", "Скрыта</span>", `href="/admin/paintings/1"`,
		`data-csrf="` + csrf + `"`, "+ Добавить картину", "Сохранено", "/static/vendor/Sortable.min.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestListEmpty(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	if body := h.get("/admin/paintings", c).Body.String(); !strings.Contains(body, "Пока нет ни одной картины") {
		t.Fatal("empty state missing")
	}
}

func TestReorder(t *testing.T) {
	h := newHarness(t)
	a := h.addPainting("A", true)
	b := h.addPainting("B", true)
	c, csrf := h.login()

	req := httptest.NewRequest(http.MethodPost, "/admin/paintings/reorder", strings.NewReader(`{"ids":[`+itoa(a.ID)+`,`+itoa(b.ID)+`]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	if rec := h.do(req, c); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	all, _ := h.st.ListPaintings(context.Background(), false)
	if all[0].Title != "A" || all[1].Title != "B" {
		t.Fatalf("order = %s, %s", all[0].Title, all[1].Title)
	}
}

func TestReorderRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	for _, body := range []string{`not json`, `{"ids":[]}`} {
		req := httptest.NewRequest(http.MethodPost, "/admin/paintings/reorder", strings.NewReader(body))
		req.Header.Set("X-CSRF-Token", csrf)
		if rec := h.do(req, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", body, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/paintings/reorder", strings.NewReader(`{"ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	if rec := h.do(req, c); rec.Code != http.StatusForbidden {
		t.Errorf("without CSRF: status = %d, want 403", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
