package web

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	p.Title = "Об авторе — " + info.ArtistName
	p.Description = truncate(info.AboutText, 160)
	if info.AboutPhoto != nil {
		p.OGImage = s.baseURL + images.URL(images.AboutDir, info.AboutPhoto.Version, 1200)
	}
	p.Nav = "about"
	s.render(w, r, http.StatusOK, "about.html", p)
}

func (s *Server) contacts(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	p.Title = "Контакты — " + info.ArtistName
	p.Description = "Как связаться с автором: " + info.ArtistName
	p.Nav = "contacts"
	s.render(w, r, http.StatusOK, "contacts.html", p)
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	paintings, err := s.store.ListPaintings(r.Context(), true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	categories, err := s.store.ListPublicCategories(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	set := sitemapSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, path := range []string{"/", "/about", "/contacts"} {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + path})
	}
	for _, c := range categories {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/category/" + c.Slug})
	}
	for _, p := range paintings {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/paintings/" + p.Slug, LastMod: p.UpdatedAt.Format("2006-01-02")})
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(set); err != nil {
		s.log.Error("write sitemap", "err", err)
	}
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin\n\nSitemap: %s/sitemap.xml\n", s.baseURL)
}
