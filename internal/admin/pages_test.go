package admin

import (
	"context"
	"image/color"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestAboutSaveText(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/about", map[string]string{"csrf": csrf, "about_text": "Строка\r\n\r\nВторая "}, nil), c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/about?msg=saved" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	info, _ := site.Load(context.Background(), h.st)
	if info.AboutText != "Строка\n\nВторая" {
		t.Fatalf("AboutText = %q", info.AboutText)
	}
	body := h.get("/admin/about?msg=saved", c).Body.String()
	if !strings.Contains(body, "Сохранено") || !strings.Contains(body, "Строка") {
		t.Error("form should show the saved text and the flash")
	}
}

func TestAboutPhotoUploadAndRecrop(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/about", map[string]string{"csrf": csrf, "about_text": "Обо мне"},
		testutil.JPEG(t, 300, 400, color.White)), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status = %d, body = %s", rec.Code, rec.Body)
	}
	body := h.get("/admin/about", c).Body.String()
	if !strings.Contains(body, "Изменить кадрирование") || !strings.Contains(body, `data-original="/admin/about/original"`) {
		t.Error("re-crop button missing after upload")
	}
	rec = h.do(multipartReq(t, "/admin/about", map[string]string{
		"csrf": csrf, "about_text": "Обо мне", "crop_changed": "1", "crop_w": "100", "crop_h": "100",
	}, nil), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("recrop: status = %d", rec.Code)
	}
	info, _ := site.Load(context.Background(), h.st)
	if info.AboutPhoto == nil || info.AboutPhoto.Version != 2 || info.AboutPhoto.Width != 100 {
		t.Fatalf("AboutPhoto = %+v", info.AboutPhoto)
	}
	if rec := h.get("/admin/about/original", c); rec.Code != http.StatusOK {
		t.Errorf("original: status = %d", rec.Code)
	}
}

func TestAboutBadPhoto(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/about", map[string]string{"csrf": csrf, "about_text": "Текст"}, []byte("nope")), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Не удалось прочитать фото") {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Текст") {
		t.Error("typed text must be kept")
	}
}

func TestSettingsSave(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	form := url.Values{
		"csrf": {csrf}, site.KeyArtistName: {" Анна Иванова "}, site.KeySubtitle: {"художник, акварель"},
		site.KeyGreeting: {"Здравствуйте!\r\n"}, site.KeyPhone: {"+7 900 000-00-00"}, site.KeyEmail: {"anna@example.ru"},
		site.KeyTelegram: {"@anna"}, site.KeyWhatsApp: {"+7 900 000-00-00"}, site.KeyVK: {"anna"},
	}
	rec := h.postForm("/admin/settings", form, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/settings?msg=saved" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	info, _ := site.Load(context.Background(), h.st)
	if info.ArtistName != "Анна Иванова" || info.Greeting != "Здравствуйте!" || info.Telegram != "@anna" {
		t.Fatalf("info = %+v", info)
	}
	if body := h.get("/admin/settings", c).Body.String(); !strings.Contains(body, `value="Анна Иванова"`) {
		t.Error("form should show saved values")
	}
}

func TestSettingsInvalidEmail(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.postForm("/admin/settings", url.Values{"csrf": {csrf}, site.KeyEmail: {"not-an-email"}, site.KeyArtistName: {"Анна"}}, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Проверьте адрес почты") {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `value="Анна"`) {
		t.Error("typed values must be kept")
	}
}

func TestChangePassword(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.postForm("/admin/password", url.Values{"csrf": {csrf}, "current": {testPassword}, "new": {"new password 1"}, "repeat": {"new password 1"}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/settings?msg=password" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	ctx := context.Background()
	if ok, _ := checkPassword(ctx, h.st, "new password 1"); !ok {
		t.Error("new password must work")
	}
	if ok, _ := checkPassword(ctx, h.st, testPassword); ok {
		t.Error("old password must stop working")
	}
}

func TestChangePasswordErrors(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	cases := []struct {
		current, newPw, repeat, want string
	}{
		{"wrong", "new password 1", "new password 1", "Неверный текущий пароль"},
		{testPassword, "short", "short", "не короче 8 символов"},
		{testPassword, "new password 1", "new password 2", "Пароли не совпадают"},
		{testPassword, strings.Repeat("я", 40), strings.Repeat("я", 40), "слишком длинный"},
		{testPassword, strings.Repeat("a", 73), strings.Repeat("a", 73), "слишком длинный"},
	}
	for _, tc := range cases {
		rec := h.postForm("/admin/password", url.Values{"csrf": {csrf}, "current": {tc.current}, "new": {tc.newPw}, "repeat": {tc.repeat}}, c)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%q: status = %d, want message %q", tc.want, rec.Code, tc.want)
		}
	}
}
