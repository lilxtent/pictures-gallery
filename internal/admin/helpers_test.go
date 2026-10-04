package admin

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

const testPassword = "correct horse battery"

type harness struct {
	t   *testing.T
	st  *store.Store
	g   *gallery.Gallery
	h   http.Handler
	now time.Time
}

func newHarness(t *testing.T, opts ...func(*Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &gallery.Gallery{Store: st, Disk: &images.Disk{Root: filepath.Join(dir, "images")}}
	if err := EnsurePassword(context.Background(), st, testPassword); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, st: st, g: g, now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	cfg := Config{Gallery: g, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return h.now }}
	for _, o := range opts {
		o(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.Register(mux)
	h.h = mux
	return h
}

func (h *harness) do(req *http.Request, c *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func (h *harness) get(path string, c *http.Cookie) *httptest.ResponseRecorder {
	return h.do(httptest.NewRequest(http.MethodGet, path, nil), c)
}

func (h *harness) postForm(path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return h.do(req, c)
}

// login signs in and returns the session cookie and the CSRF token.
func (h *harness) login() (*http.Cookie, string) {
	h.t.Helper()
	rec := h.postForm("/admin/login", url.Values{"password": {testPassword}}, nil)
	if rec.Code != http.StatusSeeOther {
		h.t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c, csrfFor(c.Value)
		}
	}
	h.t.Fatal("no session cookie")
	return nil, ""
}

func location(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Location") }
