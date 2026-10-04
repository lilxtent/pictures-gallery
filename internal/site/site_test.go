package site

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestLoadDefaults(t *testing.T) {
	info, err := Load(context.Background(), openStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.ArtistName != DefaultArtistName || info.AboutPhoto != nil || len(info.Contacts()) != 0 {
		t.Fatalf("defaults = %+v", info)
	}
}

func TestLoadReadsSettings(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	photo, _ := json.Marshal(Photo{Version: 2, Width: 300, Height: 400, Crop: images.Crop{W: 10, H: 10}})
	st.SetSettings(ctx, map[string]string{
		KeyArtistName: "Анна Иванова", KeySubtitle: "художник, акварель", KeyGreeting: "Здравствуйте!",
		KeyAboutText: "Обо мне", KeyAboutPhoto: string(photo), KeyPhone: "+7 900 000-00-00",
	})
	info, err := Load(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if info.ArtistName != "Анна Иванова" || info.Subtitle != "художник, акварель" || info.Greeting != "Здравствуйте!" ||
		info.AboutText != "Обо мне" || info.Phone != "+7 900 000-00-00" {
		t.Fatalf("info = %+v", info)
	}
	if info.AboutPhoto == nil || info.AboutPhoto.Version != 2 || info.AboutPhoto.Width != 300 {
		t.Fatalf("AboutPhoto = %+v", info.AboutPhoto)
	}
}

func TestLoadIgnoresBrokenPhotoJSON(t *testing.T) {
	st := openStore(t)
	st.SetSettings(context.Background(), map[string]string{KeyAboutPhoto: "{broken"})
	info, err := Load(context.Background(), st)
	if err != nil || info.AboutPhoto != nil {
		t.Fatalf("info.AboutPhoto = %+v, err = %v", info.AboutPhoto, err)
	}
}

func TestContacts(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want Link
	}{
		{"phone", Info{Phone: "+7 (900) 000-00-00"}, Link{"Телефон", "+7 (900) 000-00-00", "tel:+79000000000"}},
		{"email", Info{Email: "anna@example.ru"}, Link{"Почта", "anna@example.ru", "mailto:anna@example.ru"}},
		{"telegram username", Info{Telegram: "@anna_art"}, Link{"Telegram", "@anna_art", "https://t.me/anna_art"}},
		{"telegram bare", Info{Telegram: "anna_art"}, Link{"Telegram", "@anna_art", "https://t.me/anna_art"}},
		{"telegram link", Info{Telegram: "https://t.me/anna_art"}, Link{"Telegram", "t.me/anna_art", "https://t.me/anna_art"}},
		{"telegram link without scheme", Info{Telegram: "t.me/anna_art"}, Link{"Telegram", "t.me/anna_art", "https://t.me/anna_art"}},
		{"whatsapp 8", Info{WhatsApp: "8 900 000-00-00"}, Link{"WhatsApp", "8 900 000-00-00", "https://wa.me/79000000000"}},
		{"whatsapp +7", Info{WhatsApp: "+7 900 000 00 00"}, Link{"WhatsApp", "+7 900 000 00 00", "https://wa.me/79000000000"}},
		{"vk id", Info{VK: "anna.art"}, Link{"ВКонтакте", "vk.com/anna.art", "https://vk.com/anna.art"}},
		{"vk link", Info{VK: "https://vk.com/anna.art"}, Link{"ВКонтакте", "vk.com/anna.art", "https://vk.com/anna.art"}},
		{"vk link without scheme", Info{VK: "vk.com/anna.art"}, Link{"ВКонтакте", "vk.com/anna.art", "https://vk.com/anna.art"}},
	}
	for _, tt := range tests {
		got := tt.info.Contacts()
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestContactsOrderAndSkipsEmpty(t *testing.T) {
	info := Info{VK: "x", Phone: "1", Email: "a@b.c", Telegram: "  "}
	got := info.Contacts()
	if len(got) != 3 || got[0].Label != "Телефон" || got[1].Label != "Почта" || got[2].Label != "ВКонтакте" {
		t.Fatalf("got %+v", got)
	}
}
