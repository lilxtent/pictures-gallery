package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

func TestEnsurePassword(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Already set by the harness: a different initial password must not replace it.
	if err := EnsurePassword(ctx, h.st, "something else"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := checkPassword(ctx, h.st, testPassword); !ok {
		t.Fatal("original password must still work")
	}
}

func TestEnsurePasswordRequiresInitialValue(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/x.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := EnsurePassword(context.Background(), st, ""); err == nil {
		t.Fatal("expected an error when no password is stored and none is given")
	}
}

func TestLoginPage(t *testing.T) {
	rec := newHarness(t).get("/admin/login", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Вход") {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestProtectedPagesRedirectToLogin(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/admin/", "/admin/whatever"} {
		rec := h.get(path, nil)
		if rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
			t.Errorf("%s: status = %d, location = %q", path, rec.Code, location(rec))
		}
	}
}

func TestLoginWrongPassword(t *testing.T) {
	rec := newHarness(t).postForm("/admin/login", url.Values{"password": {"nope"}}, nil)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Неверный пароль") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("no cookie on failed login")
	}
}

func TestLoginSuccess(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	if !c.HttpOnly || c.Path != "/admin" || c.SameSite != http.SameSiteLaxMode || c.Secure {
		t.Errorf("cookie = %+v", c)
	}
	rec := h.get("/admin/", c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	if rec := h.get("/admin/login", c); location(rec) != "/admin/paintings" {
		t.Errorf("logged-in user should be sent from login to paintings, got %q", location(rec))
	}
}

func TestSecureCookies(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SecureCookies = true })
	c, _ := h.login()
	if !c.Secure {
		t.Fatal("cookie must be Secure")
	}
}

func TestSessionExpires(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	h.now = h.now.Add(31 * 24 * time.Hour)
	if rec := h.get("/admin/", c); location(rec) != "/admin/login" {
		t.Fatalf("expired session: location = %q", location(rec))
	}
}

func TestUnknownAdminPageIs404WhenLoggedIn(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	rec := h.get("/admin/nope", c)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Страница не найдена") {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestLogoutRequiresCSRF(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()

	rec := h.postForm("/admin/logout", url.Values{}, c)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Страница устарела") {
		t.Fatalf("without csrf: status = %d", rec.Code)
	}

	rec = h.postForm("/admin/logout", url.Values{"csrf": {csrf}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
		t.Fatalf("with csrf: status = %d, location = %q", rec.Code, location(rec))
	}
	if rec := h.get("/admin/", c); location(rec) != "/admin/login" {
		t.Fatal("session must be gone after logout")
	}
}

func TestCSRFHeaderIsAccepted(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	req.Header.Set("X-CSRF-Token", csrf)
	if rec := h.do(req, c); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 5; i++ {
		if rec := h.postForm("/admin/login", url.Values{"password": {"wrong"}}, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d", i+1, rec.Code)
		}
	}
	rec := h.postForm("/admin/login", url.Values{"password": {testPassword}}, nil)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Слишком много попыток") {
		t.Fatalf("6th attempt: status = %d", rec.Code)
	}
	h.now = h.now.Add(16 * time.Minute)
	h.login() // works again
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:5555"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if got := clientIP(req, false); got != "10.0.0.2" {
		t.Errorf("untrusted: got %q", got)
	}
	if got := clientIP(req, true); got != "203.0.113.9" {
		t.Errorf("trusted: got %q", got)
	}
	req.Header.Del("X-Forwarded-For")
	if got := clientIP(req, true); got != "10.0.0.2" {
		t.Errorf("trusted without header: got %q", got)
	}
}
