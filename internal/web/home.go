package web

import (
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

type homeData struct {
	Paintings []store.Painting
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	info, err := site.Load(ctx, s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	paintings, err := s.store.ListPaintings(ctx, true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	if info.Subtitle != "" {
		p.Title = info.ArtistName + " — " + info.Subtitle
	}
	p.Description = truncate(info.Greeting, 160)
	if p.Description == "" {
		p.Description = "Картины и истории их создания. " + info.ArtistName
	}
	if len(paintings) > 0 {
		first := paintings[0]
		p.OGImage = s.baseURL + imagesURL1200(first)
	}
	p.Nav = "works"
	p.Data = homeData{Paintings: paintings}
	s.render(w, r, http.StatusOK, "home.html", p)
}
