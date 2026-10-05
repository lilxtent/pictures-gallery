package web

import (
	"errors"
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

// homeData is the data for the gallery grid, on the home page (Current is
// nil) and on a category page.
type homeData struct {
	Paintings  []store.Painting
	Categories []store.Category // tabs: only categories with visible paintings
	Current    *store.Category
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.gallery(w, r, nil)
}

func (s *Server) category(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCategoryBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.gallery(w, r, &c)
}

// gallery renders the grid of all visible paintings, or only those of current.
func (s *Server) gallery(w http.ResponseWriter, r *http.Request, current *store.Category) {
	ctx := r.Context()
	info, err := site.Load(ctx, s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var paintings []store.Painting
	if current != nil {
		paintings, err = s.store.ListVisiblePaintingsInCategory(ctx, current.ID)
	} else {
		paintings, err = s.store.ListPaintings(ctx, true)
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// A category without visible paintings is not listed, so it has no page either.
	if current != nil && len(paintings) == 0 {
		s.notFound(w, r)
		return
	}
	categories, err := s.store.ListPublicCategories(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	if current != nil {
		p.Title = current.Name + " — " + info.ArtistName
		p.Description = "Картины в категории «" + current.Name + "». " + info.ArtistName
	} else {
		if info.Subtitle != "" {
			p.Title = info.ArtistName + " — " + info.Subtitle
		}
		p.Description = truncate(info.Greeting, 160)
		if p.Description == "" {
			p.Description = "Картины и истории их создания. " + info.ArtistName
		}
	}
	if len(paintings) > 0 {
		first := paintings[0]
		p.OGImage = s.baseURL + imagesURL1200(first)
	}
	p.Nav = "works"
	p.Data = homeData{Paintings: paintings, Categories: categories, Current: current}
	s.render(w, r, http.StatusOK, "home.html", p)
}
