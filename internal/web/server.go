// Package web serves the public site.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"

	"github.com/lilxtent/pictures-gallery/internal/assets"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

// Config holds the server's dependencies.
type Config struct {
	Store   *store.Store
	Disk    *images.Disk
	BaseURL string // absolute site URL without trailing slash, for OG tags and sitemap
	Log     *slog.Logger
}

// Server renders the public pages.
type Server struct {
	store   *store.Store
	disk    *images.Disk
	baseURL string
	log     *slog.Logger
	tpl     map[string]*template.Template
}

// New parses the templates and returns a ready server.
func New(cfg Config) (*Server, error) {
	tpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{store: cfg.Store, disk: cfg.Disk, baseURL: cfg.BaseURL, log: cfg.Log, tpl: tpl}, nil
}

func parseTemplates() (map[string]*template.Template, error) {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	out := map[string]*template.Template{}
	for _, n := range names {
		base := path.Base(n)
		if base == "layout.html" {
			continue
		}
		t, err := template.New(base).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", n)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", base, err)
		}
		out[base] = t
	}
	return out, nil
}

// Register adds the public routes, /static/ and the catch-all 404 to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /category/{slug}", s.category)
	mux.HandleFunc("GET /paintings/{slug}", s.painting)
	mux.HandleFunc("GET /media/paintings/{id}/{file}", s.media)
	mux.HandleFunc("GET /media/about/{file}", s.media)
	mux.HandleFunc("GET /about", s.about)
	mux.HandleFunc("GET /contacts", s.contacts)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.Handle("GET /static/", assets.Handler())
	mux.HandleFunc("/", s.notFound)
}

// page is the data every template receives.
type page struct {
	Site         site.Info
	Title        string
	Description  string
	CanonicalURL string
	OGImage      string
	OGType       string
	Nav          string // "works", "about", "contacts"
	Data         any
}

func (s *Server) newPage(r *http.Request, info site.Info) page {
	return page{Site: info, Title: info.ArtistName, CanonicalURL: s.baseURL + r.URL.Path, OGType: "website"}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	t, ok := s.tpl[name]
	if !ok {
		s.log.Error("unknown template", "name", name)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", p); err != nil {
		s.log.Error("render failed", "template", name, "path", r.URL.Path, "err", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), s.store)
	if err != nil {
		s.log.Error("load site info", "err", err)
		info = site.Info{ArtistName: site.DefaultArtistName}
	}
	p := s.newPage(r, info)
	p.Title = "Страница не найдена — " + info.ArtistName
	s.render(w, r, http.StatusNotFound, "notfound.html", p)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	info, lerr := site.Load(r.Context(), s.store)
	if lerr != nil {
		info = site.Info{ArtistName: site.DefaultArtistName}
	}
	p := s.newPage(r, info)
	p.Title = "Ошибка — " + info.ArtistName
	s.render(w, r, http.StatusInternalServerError, "error.html", p)
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	dir := images.AboutDir
	if id := r.PathValue("id"); id != "" {
		dir = "paintings/" + id
	}
	file, ok := s.disk.VariantFile(dir, r.PathValue("file"))
	if !ok || !isFile(file) {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, file)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
