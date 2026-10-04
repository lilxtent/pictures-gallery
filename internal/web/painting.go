package web

import (
	"errors"
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

type paintingData struct {
	Painting   store.Painting
	Prev, Next *store.Painting
}

func (s *Server) painting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPaintingBySlug(ctx, r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.Visible) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	info, err := site.Load(ctx, s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	all, err := s.store.ListPaintings(ctx, true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := paintingData{Painting: p}
	for i := range all {
		if all[i].ID != p.ID {
			continue
		}
		if i > 0 {
			data.Prev = &all[i-1]
		}
		if i < len(all)-1 {
			data.Next = &all[i+1]
		}
		break
	}

	pg := s.newPage(r, info)
	pg.Title = p.Title + " — " + info.ArtistName
	pg.Description = truncate(p.Description, 160)
	if pg.Description == "" {
		pg.Description = details(p)
	}
	pg.OGType = "article"
	pg.OGImage = s.baseURL + imagesURL1200(p)
	pg.Nav = "works"
	pg.Data = data
	s.render(w, r, http.StatusOK, "painting.html", pg)
}
