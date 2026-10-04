package web

import (
	"reflect"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

func TestParagraphs(t *testing.T) {
	got := paragraphs("Первый абзац\nвторая строка\r\n\r\n\n  Второй  \n \n")
	want := []string{"Первый абзац\nвторая строка", "Второй"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(paragraphs("  \n ")) != 0 {
		t.Fatal("blank text has no paragraphs")
	}
}

func TestDetails(t *testing.T) {
	p := store.Painting{Technique: "Бумага, акварель", Size: "30×40 см", Year: 2025}
	if got := details(p); got != "Бумага, акварель · 30×40 см · 2025" {
		t.Errorf("got %q", got)
	}
	if got := details(store.Painting{Size: "30×40 см"}); got != "30×40 см" {
		t.Errorf("got %q", got)
	}
	if got := details(store.Painting{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("короткий  текст\n", 50); got != "короткий текст" {
		t.Errorf("got %q", got)
	}
	if got := truncate("один два три", 7); got != "один д…" {
		t.Errorf("got %q", got)
	}
}
