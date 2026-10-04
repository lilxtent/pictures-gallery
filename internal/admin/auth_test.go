package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

func TestWrongCSRFFormFieldIsRejected(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.postForm("/admin/logout", url.Values{"csrf": {csrf + "x"}}, c)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec := h.get("/admin/", c); location(rec) != "/admin/paintings" {
		t.Fatal("session must survive a rejected logout")
	}
}

func TestCSRFHeaderTakesPrecedenceOverFormField(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "wrong")
	if rec := h.do(req, c); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestUnauthenticatedPostRedirectsWithoutReachingHandler(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	// Valid CSRF token but no session cookie: must not log anyone out or run the handler.
	rec := h.postForm("/admin/logout", url.Values{"csrf": {csrf}}, nil)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("the logout handler must not have run")
	}
	if rec := h.get("/admin/", c); location(rec) != "/admin/paintings" {
		t.Fatal("the real session must be untouched")
	}
}

func TestSessionStoredAsHash(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	ctx := context.Background()
	if ok, err := h.st.SessionValid(ctx, hashToken(c.Value), h.now); err != nil || !ok {
		t.Fatalf("hash of the cookie value must be stored: ok = %v, err = %v", ok, err)
	}
	if ok, _ := h.st.SessionValid(ctx, c.Value, h.now); ok {
		t.Fatal("the raw token must not be stored")
	}
}

func loginFrom(h *harness, remoteAddr, password string) int {
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(url.Values{"password": {password}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	return h.do(req, nil).Code
}

func TestLoginRateLimitSlidingWindowAndPerIP(t *testing.T) {
	h := newHarness(t)
	const ip = "198.51.100.7:4000"
	start := h.now
	for _, off := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute, 11 * time.Minute, 12 * time.Minute} {
		h.now = start.Add(off)
		if code := loginFrom(h, ip, "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("failure at +%v: status = %d", off, code)
		}
	}
	h.now = start.Add(14 * time.Minute)
	if code := loginFrom(h, ip, "wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("inside the window: status = %d", code)
	}
	// A different IP is not affected.
	if code := loginFrom(h, "198.51.100.8:4000", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("other IP: status = %d", code)
	}
	// Only the failure at +0 has aged out at +15m: exactly one more attempt fits.
	h.now = start.Add(15 * time.Minute)
	if code := loginFrom(h, ip, "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("after the oldest aged out: status = %d", code)
	}
	if code := loginFrom(h, ip, "wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("second attempt after one aged out: status = %d", code)
	}
}

func TestLoginRateLimitHoldsUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	const n = 20
	var wg sync.WaitGroup
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- loginFrom(h, "198.51.100.9:4000", "wrong")
		}()
	}
	wg.Wait()
	close(codes)
	var unauthorized, limited int
	for code := range codes {
		switch code {
		case http.StatusUnauthorized:
			unauthorized++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if unauthorized > 5 || unauthorized+limited != n {
		t.Fatalf("401s = %d, 429s = %d; at most 5 attempts may be checked", unauthorized, limited)
	}
}
