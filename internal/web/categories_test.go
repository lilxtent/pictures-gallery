package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
)

func TestHomeHasNoTabsWithoutCategories(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	if body := e.get("/").Body.String(); strings.Contains(body, `class="tabs"`) {
		t.Error("tabs must be hidden when there are no categories")
	}
}

func TestHomeShowsTabsForCategoriesWithVisiblePaintings(t *testing.T) {
	e := newEnv(t)
	land := e.category("Пейзажи")
	hidden := e.category("Скрытая")
	e.category("Пустая")
	e.addIn(land, "Река", true)
	e.addIn(hidden, "Тайна", false)
	body := e.get("/").Body.String()
	for _, want := range []string{`class="tabs"`, `href="/category/peyzazhi"`, `aria-current="page">Все`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	for _, bad := range []string{"Скрытая", "Пустая"} {
		if strings.Contains(body, bad) {
			t.Errorf("tab %q must not be shown", bad)
		}
	}
}

func TestHomeTabsFollowCategoryOrder(t *testing.T) {
	e := newEnv(t)
	a := e.category("Альфа")
	b := e.category("Бета")
	e.addIn(a, "Раз", true)
	e.addIn(b, "Два", true)
	if err := e.st.ReorderCategories(t.Context(), []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	body := e.get("/").Body.String()
	if ib, ia := strings.Index(body, ">Бета<"), strings.Index(body, ">Альфа<"); ib < 0 || ia < 0 || ib > ia {
		t.Errorf("want Бета before Альфа, got indexes %d, %d", ib, ia)
	}
}

func TestCategoryPageListsOnlyItsVisiblePaintings(t *testing.T) {
	e := newEnv(t)
	land := e.category("Пейзажи")
	other := e.category("Портреты")
	e.addIn(land, "Река", true)
	e.addIn(land, "Тайный лес", false)
	e.addIn(other, "Дама", true)
	e.add(gallery.PaintingInput{Title: "Без категории", Visible: true})

	rec := e.get("/category/peyzazhi")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "Река") {
		t.Error("category painting should be listed")
	}
	for _, bad := range []string{"Тайный лес", "Дама", "Без категории"} {
		if strings.Contains(body, bad) {
			t.Errorf("%q must not be listed", bad)
		}
	}
	for _, want := range []string{"<title>Пейзажи — ", `href="https://example.ru/category/peyzazhi"`,
		`href="/paintings/reka?category=peyzazhi"`, `href="/category/peyzazhi" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, `class="intro"`) {
		t.Error("the greeting belongs to the home page only")
	}
}

func TestCategoryPageNotFound(t *testing.T) {
	e := newEnv(t)
	e.category("Пустая")
	hidden := e.category("Скрытая")
	e.addIn(hidden, "Тайна", false)
	for _, path := range []string{"/category/nope", "/category/pustaya", "/category/skrytaya"} {
		if rec := e.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestPaintingPrevNextScopedToCategory(t *testing.T) {
	e := newEnv(t)
	land := e.category("Пейзажи")
	e.addIn(land, "Первая", true)
	e.add(gallery.PaintingInput{Title: "Чужая", Visible: true})
	e.addIn(land, "Вторая", true)
	// Site order, newest first: Вторая, Чужая, Первая.

	scoped := e.get("/paintings/pervaya?category=peyzazhi").Body.String()
	if !strings.Contains(scoped, `href="/paintings/vtoraya?category=peyzazhi"`) {
		t.Error("prev should be the previous painting of the category")
	}
	if strings.Contains(scoped, "Чужая") {
		t.Error("a painting outside the category must not be linked")
	}
	if !strings.Contains(scoped, `class="back" href="/category/peyzazhi"`) {
		t.Error("back link should return to the category")
	}

	plain := e.get("/paintings/pervaya").Body.String()
	if !strings.Contains(plain, `href="/paintings/chuzhaya"`) || !strings.Contains(plain, `class="back" href="/"`) {
		t.Error("without the query, navigation spans all works")
	}
}

func TestPaintingIgnoresCategoryItDoesNotBelongTo(t *testing.T) {
	e := newEnv(t)
	land := e.category("Пейзажи")
	e.category("Портреты")
	e.addIn(land, "Река", true)
	e.add(gallery.PaintingInput{Title: "Дама", Visible: true})
	for _, query := range []string{"?category=portrety", "?category=nope"} {
		body := e.get("/paintings/reka" + query).Body.String()
		if !strings.Contains(body, `class="back" href="/"`) || !strings.Contains(body, `href="/paintings/dama"`) {
			t.Errorf("%s: should fall back to all works", query)
		}
	}
	// An uncategorised painting ignores the parameter altogether.
	if body := e.get("/paintings/dama?category=peyzazhi").Body.String(); !strings.Contains(body, `class="back" href="/"`) {
		t.Error("uncategorised painting should fall back to all works")
	}
}

func TestSitemapListsCategoriesWithVisiblePaintings(t *testing.T) {
	e := newEnv(t)
	land := e.category("Пейзажи")
	hidden := e.category("Скрытая")
	e.category("Пустая")
	e.addIn(land, "Река", true)
	e.addIn(hidden, "Тайна", false)
	body := e.get("/sitemap.xml").Body.String()
	if !strings.Contains(body, "<loc>https://example.ru/category/peyzazhi</loc>") {
		t.Errorf("sitemap should list the category, got %s", body)
	}
	for _, bad := range []string{"skrytaya", "pustaya"} {
		if strings.Contains(body, bad) {
			t.Errorf("sitemap must not contain %q", bad)
		}
	}
}
