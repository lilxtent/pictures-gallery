package slug

import (
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Жёлтая лилия", "zheltaya-liliya"},
		{"Карпы кои", "karpy-koi"},
		{"Пион", "pion"},
		{"  Три   мака!!! ", "tri-maka"},
		{"Объявление", "obyavlenie"},
		{"Щука и ёж", "shchuka-i-ezh"},
		{"Зима 2024", "zima-2024"},
		{"Summer Sea", "summer-sea"},
		{"Ночь—день", "noch-den"},
		{"Й", "y"},
		{"!!!", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Make(tt.in); got != tt.want {
			t.Errorf("Make(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMakeTruncatesLongTitles(t *testing.T) {
	got := Make(strings.Repeat("а", 100))
	if got != strings.Repeat("a", 80) {
		t.Fatalf("got %q (len %d), want 80 a's", got, len(got))
	}
}

func TestMakeTruncationDoesNotLeaveTrailingDash(t *testing.T) {
	got := Make(strings.Repeat("a", 79) + " b")
	if strings.HasSuffix(got, "-") {
		t.Fatalf("got %q, must not end with a dash", got)
	}
}
