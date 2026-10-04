// Package admin serves the content-management pages under /admin.
package admin

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

const (
	maxPhotoBytes   = 30 << 20
	maxRequestBytes = maxPhotoBytes + 2<<20
)

// Config holds the admin's dependencies.
type Config struct {
	Gallery       *gallery.Gallery
	Log           *slog.Logger
	SecureCookies bool             // false only for local development over http
	TrustProxy    bool             // take the client IP from X-Forwarded-For
	Now           func() time.Time // defaults to time.Now
}

// Admin serves /admin.
type Admin struct {
	g          *gallery.Gallery
	st         *store.Store
	log        *slog.Logger
	secure     bool
	trustProxy bool
	now        func() time.Time
	limiter    *limiter
	tpl        map[string]*template.Template
}

// New parses the templates and returns a ready admin.
func New(cfg Config) (*Admin, error) {
	tpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Admin{
		g: cfg.Gallery, st: cfg.Gallery.Store, log: cfg.Log,
		secure: cfg.SecureCookies, trustProxy: cfg.TrustProxy, now: now,
		limiter: newLimiter(5, 15*time.Minute), tpl: tpl,
	}, nil
}

var funcs = template.FuncMap{
	"imgURL":      images.URL,
	"paintingDir": images.PaintingDir,
}

func parseTemplates() (map[string]*template.Template, error) {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	var partials []string
	for _, n := range names {
		if strings.HasPrefix(path.Base(n), "_") {
			partials = append(partials, n)
		}
	}
	out := map[string]*template.Template{}
	for _, n := range names {
		base := path.Base(n)
		if base == "layout.html" || strings.HasPrefix(base, "_") {
			continue
		}
		files := append(append([]string{"templates/layout.html"}, partials...), n)
		t, err := template.New(base).Funcs(funcs).ParseFS(templateFS, files...)
		if err != nil {
			return nil, fmt.Errorf("admin: parse %s: %w", base, err)
		}
		out[base] = t
	}
	return out, nil
}

// Register adds the admin routes to mux.
func (a *Admin) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/login", a.loginPage)
	mux.HandleFunc("POST /admin/login", a.login)
	mux.HandleFunc("POST /admin/logout", a.requireAuth(a.logout))
	mux.HandleFunc("GET /admin/{$}", a.requireAuth(a.index))
	mux.HandleFunc("GET /admin/paintings", a.requireAuth(a.list))
	mux.HandleFunc("POST /admin/paintings/reorder", a.requireAuth(a.reorder))
	mux.HandleFunc("/admin/", a.requireAuth(a.notFound))
}

// view is the data every admin template receives.
type view struct {
	Title string
	CSRF  string // empty on pages shown to logged-out users; hides the menu
	Nav   string // "paintings", "about", "settings"
	Flash string
	Data  any
}

var flashes = map[string]string{
	"saved":    "Сохранено",
	"deleted":  "Картина удалена",
	"password": "Пароль изменён",
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
}

func (a *Admin) render(w http.ResponseWriter, r *http.Request, status int, name string, v view) {
	if v.CSRF == "" {
		v.CSRF = csrfToken(r)
	}
	if v.Flash == "" {
		v.Flash = flashes[r.URL.Query().Get("msg")]
	}
	t, ok := a.tpl[name]
	if !ok {
		a.log.Error("unknown template", "name", name)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		a.log.Error("render failed", "template", name, "err", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	noCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *Admin) renderMessage(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	a.render(w, r, status, "message.html", view{Title: title, Data: msg})
}

func (a *Admin) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderMessage(w, r, http.StatusNotFound, "Страница не найдена", "Такой страницы нет.")
}

func (a *Admin) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("admin request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	a.renderMessage(w, r, http.StatusInternalServerError, "Ошибка", "Что-то пошло не так. Попробуйте ещё раз чуть позже.")
}

func (a *Admin) index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/paintings", http.StatusSeeOther)
}
