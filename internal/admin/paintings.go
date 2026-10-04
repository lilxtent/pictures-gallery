package admin

import (
	"encoding/json"
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

func (a *Admin) list(w http.ResponseWriter, r *http.Request) {
	paintings, err := a.st.ListPaintings(r.Context(), false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "paintings.html", view{Title: "Картины", Nav: "paintings", Data: paintings})
}

func (a *Admin) reorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := a.st.ReorderPaintings(r.Context(), body.IDs); err != nil {
		a.log.Error("reorder", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type formView struct {
	Painting *store.Painting // nil when adding a new painting
	Input    gallery.PaintingInput
	YearText string
	Errors   gallery.FieldErrors
	Crop     cropField
}

func (a *Admin) renderForm(w http.ResponseWriter, r *http.Request, status int, fv formView) {
	title := "Новая картина"
	if fv.Painting != nil {
		title = fv.Painting.Title
	}
	a.render(w, r, status, "painting_form.html", view{Title: title, Nav: "paintings", Data: fv})
}

func (a *Admin) newForm(w http.ResponseWriter, r *http.Request) {
	a.renderForm(w, r, http.StatusOK, formView{
		Input: gallery.PaintingInput{Visible: true},
		Crop:  cropField{Required: true},
	})
}

func (a *Admin) create(w http.ResponseWriter, r *http.Request) {
	in, yearText, errs := parsePaintingForm(r)
	merge(errs, in.Validate(a.now()))
	data, err := readPhoto(r)
	switch {
	case err != nil:
		errs["photo"] = photoError(err)
		if errs["photo"] == "" {
			errs["photo"] = msgPhotoUnreadable
		}
	case data == nil:
		errs["photo"] = msgPhotoMissing
	}
	if len(errs) == 0 {
		_, err := a.g.AddPainting(r.Context(), in, gallery.Photo{Original: data, Crop: parseCrop(r)})
		if err == nil {
			http.Redirect(w, r, "/admin/paintings?msg=saved", http.StatusSeeOther)
			return
		}
		msg := photoError(err)
		if msg == "" {
			a.serverError(w, r, err)
			return
		}
		errs["photo"] = msg
	} else if _, bad := errs["photo"]; !bad {
		errs["photo"] = msgPhotoAgain
	}
	a.renderForm(w, r, http.StatusUnprocessableEntity, formView{
		Input: in, YearText: yearText, Errors: errs,
		Crop: cropField{Required: true, Error: errs["photo"]},
	})
}
