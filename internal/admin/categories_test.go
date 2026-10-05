package admin

import (
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func (h *harness) addCategory(name string) store.Category {
	h.t.Helper()
	c := store.Category{Name: name}
	if err := h.st.CreateCategory(context.Background(), &c); err != nil {
		h.t.Fatal(err)
	}
	return c
}

func TestCategoriesRequireLogin(t *testing.T) {
	h := newHarness(t)
	if rec := h.get("/admin/categories", nil); location(rec) != "/admin/login" {
		t.Fatalf("location = %q", location(rec))
	}
	if rec := h.postForm("/admin/categories", url.Values{"name": {"x"}}, nil); location(rec) != "/admin/login" {
		t.Fatalf("POST location = %q", location(rec))
	}
}

func TestCategoriesPageListsCategoriesWithCounts(t *testing.T) {
	h := newHarness(t)
	cat := h.addCategory("Пейзажи")
	p := h.addPainting("Река", true)
	p.CategoryID = &cat.ID
	if err := h.st.UpdatePainting(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	c, _ := h.login()
	body := h.get("/admin/categories", c).Body.String()
	for _, want := range []string{`value="Пейзажи"`, "Картин: 1", "/admin/categories/1/delete", `data-url="/admin/categories/reorder"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestCreateCategory(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.postForm("/admin/categories", url.Values{"csrf": {csrf}, "name": {"  Морские   пейзажи "}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/categories?msg=saved" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, _ := h.st.ListCategories(context.Background())
	if len(got) != 1 || got[0].Name != "Морские пейзажи" {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateCategoryRejectsBadNames(t *testing.T) {
	h := newHarness(t)
	h.addCategory("Пейзажи")
	c, csrf := h.login()
	for name, want := range map[string]string{
		"   ":                    msgCategoryNameRequired,
		strings.Repeat("я", 101): msgCategoryNameTooLong,
		"пейзажи":                msgCategoryNameTaken,
	} {
		rec := h.postForm("/admin/categories", url.Values{"csrf": {csrf}, "name": {name}}, c)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%q: status = %d, want 422 with %q", name, rec.Code, want)
		}
	}
	if got, _ := h.st.ListCategories(context.Background()); len(got) != 1 {
		t.Fatalf("nothing must be created, got %d categories", len(got))
	}
}

func TestCreateCategoryNeedsCSRF(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	if rec := h.postForm("/admin/categories", url.Values{"name": {"x"}}, c); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRenameCategory(t *testing.T) {
	h := newHarness(t)
	cat := h.addCategory("Море")
	c, csrf := h.login()
	rec := h.postForm("/admin/categories/1", url.Values{"csrf": {csrf}, "name": {"Океан"}}, c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, _ := h.st.GetCategory(context.Background(), cat.ID)
	if got.Name != "Океан" || got.Slug != cat.Slug {
		t.Fatalf("got %+v", got)
	}
}

func TestRenameCategoryKeepingOwnNameIsAllowed(t *testing.T) {
	h := newHarness(t)
	h.addCategory("Море")
	c, csrf := h.login()
	rec := h.postForm("/admin/categories/1", url.Values{"csrf": {csrf}, "name": {"МОРЕ"}}, c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestRenameCategoryRejectsTakenNameAndShowsErrorOnItsRow(t *testing.T) {
	h := newHarness(t)
	h.addCategory("Море")
	h.addCategory("Лес")
	c, csrf := h.login()
	rec := h.postForm("/admin/categories/2", url.Values{"csrf": {csrf}, "name": {"море"}}, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), msgCategoryNameTaken) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if got, _ := h.st.GetCategory(context.Background(), 2); got.Name != "Лес" {
		t.Fatalf("name must stay, got %q", got.Name)
	}
}

func TestRenameUnknownCategory(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	for _, path := range []string{"/admin/categories/99", "/admin/categories/abc"} {
		if rec := h.postForm(path, url.Values{"csrf": {csrf}, "name": {"x"}}, c); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d", path, rec.Code)
		}
	}
}

func TestDeleteCategoryKeepsPaintings(t *testing.T) {
	h := newHarness(t)
	cat := h.addCategory("Море")
	p := h.addPainting("Волна", true)
	p.CategoryID = &cat.ID
	if err := h.st.UpdatePainting(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	c, csrf := h.login()

	confirm := h.get("/admin/categories/1/delete", c)
	if confirm.Code != http.StatusOK || !strings.Contains(confirm.Body.String(), "останутся на сайте") {
		t.Fatalf("confirm page: status = %d, body = %s", confirm.Code, confirm.Body)
	}
	rec := h.postForm("/admin/categories/1/delete", url.Values{"csrf": {csrf}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/categories?msg=category-deleted" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	if _, err := h.st.GetCategory(context.Background(), cat.ID); err == nil {
		t.Fatal("category must be gone")
	}
	got, err := h.st.GetPainting(context.Background(), p.ID)
	if err != nil || got.CategoryID != nil {
		t.Fatalf("painting must stay uncategorised: %+v, %v", got, err)
	}
}

func TestDeleteUnknownCategory(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	if rec := h.get("/admin/categories/99/delete", c); rec.Code != http.StatusNotFound {
		t.Errorf("GET: status = %d", rec.Code)
	}
	if rec := h.postForm("/admin/categories/99/delete", url.Values{"csrf": {csrf}}, c); rec.Code != http.StatusNotFound {
		t.Errorf("POST: status = %d", rec.Code)
	}
}

func TestReorderCategories(t *testing.T) {
	h := newHarness(t)
	a := h.addCategory("A")
	b := h.addCategory("B")
	c, csrf := h.login()
	req := httptest.NewRequest(http.MethodPost, "/admin/categories/reorder", strings.NewReader(`{"ids":[`+itoa(b.ID)+`,`+itoa(a.ID)+`]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	if rec := h.do(req, c); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	got, _ := h.st.ListCategories(context.Background())
	if got[0].Name != "B" || got[1].Name != "A" {
		t.Fatalf("order = %s, %s", got[0].Name, got[1].Name)
	}
}

func TestReorderCategoriesRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	for _, body := range []string{`not json`, `{"ids":[]}`} {
		req := httptest.NewRequest(http.MethodPost, "/admin/categories/reorder", strings.NewReader(body))
		req.Header.Set("X-CSRF-Token", csrf)
		if rec := h.do(req, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestPaintingFormOffersCategories(t *testing.T) {
	h := newHarness(t)
	cat := h.addCategory("Пейзажи")
	p := h.addPainting("Река", true)
	p.CategoryID = &cat.ID
	if err := h.st.UpdatePainting(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	h.addCategory("Портреты")
	c, _ := h.login()
	body := h.get("/admin/paintings/1", c).Body.String()
	for _, want := range []string{`name="category"`, "Без категории", `<option value="1" selected>Пейзажи</option>`, `<option value="2">Портреты</option>`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form should contain %q", want)
		}
	}
	if !strings.Contains(h.get("/admin/paintings/new", c).Body.String(), `name="category"`) {
		t.Error("new form should have the category select")
	}
}

func TestUpdateSetsAndClearsCategory(t *testing.T) {
	h := newHarness(t)
	h.addCategory("Пейзажи")
	h.addPainting("Река", true)
	c, csrf := h.login()
	ctx := context.Background()

	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Река", "category": "1"}, nil), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if p, _ := h.st.GetPainting(ctx, 1); p.CategoryID == nil || *p.CategoryID != 1 {
		t.Fatalf("category must be set, got %v", p.CategoryID)
	}

	rec = h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Река", "category": ""}, nil), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if p, _ := h.st.GetPainting(ctx, 1); p.CategoryID != nil {
		t.Fatalf("category must be cleared, got %d", *p.CategoryID)
	}
}

func TestUpdateRejectsUnknownCategory(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Река", true)
	c, csrf := h.login()
	for _, value := range []string{"99", "abc"} {
		rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Река", "category": value}, nil), c)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), msgCategoryUnknown) {
			t.Errorf("category=%q: status = %d", value, rec.Code)
		}
	}
}

func TestPaintingsListShowsCategoryName(t *testing.T) {
	h := newHarness(t)
	cat := h.addCategory("Пейзажи")
	p := h.addPainting("Река", true)
	h.addPainting("Без темы", true)
	p.CategoryID = &cat.ID
	if err := h.st.UpdatePainting(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	c, _ := h.login()
	body := h.get("/admin/paintings", c).Body.String()
	if !strings.Contains(body, `<span class="badge">Пейзажи</span>`) {
		t.Errorf("list should show the category badge, body = %s", body)
	}
}

func TestCreatePaintingInCategory(t *testing.T) {
	h := newHarness(t)
	h.addCategory("Пейзажи")
	c, csrf := h.login()
	req := multipartReq(t, "/admin/paintings/new", map[string]string{
		"csrf": csrf, "title": "Пион", "visible": "on", "category": "1",
		"crop_x": "0", "crop_y": "0", "crop_w": "100", "crop_h": "100", "crop_rotate": "0", "crop_changed": "1",
	}, testutil.JPEG(t, 300, 200, color.White))
	if rec := h.do(req, c); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	p, err := h.st.GetPainting(context.Background(), 1)
	if err != nil || p.CategoryID == nil || *p.CategoryID != 1 {
		t.Fatalf("got %+v, %v", p, err)
	}
}

func TestCreateKeepsChosenCategoryOnValidationError(t *testing.T) {
	h := newHarness(t)
	h.addCategory("Пейзажи")
	h.addCategory("Портреты")
	c, csrf := h.login()
	req := multipartReq(t, "/admin/paintings/new", map[string]string{"csrf": csrf, "title": " ", "category": "2"},
		testutil.JPEG(t, 30, 20, color.White))
	rec := h.do(req, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `<option value="2" selected>`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}
