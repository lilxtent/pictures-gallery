package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
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

// paintingFromPath loads the painting named by the {id} path segment,
// writing a 404/500 response and returning false when it cannot.
func (a *Admin) paintingFromPath(w http.ResponseWriter, r *http.Request) (store.Painting, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return store.Painting{}, false
	}
	p, err := a.st.GetPainting(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return store.Painting{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.Painting{}, false
	}
	return p, true
}

func editCrop(p store.Painting, errMsg string) cropField {
	return cropField{
		CurrentURL:  images.URL(images.PaintingDir(p.ID), p.ImageVersion, 600),
		OriginalURL: "/admin/paintings/" + strconv.FormatInt(p.ID, 10) + "/original",
		CropJSON:    cropJSON(p.Crop),
		Error:       errMsg,
	}
}

func (a *Admin) editForm(w http.ResponseWriter, r *http.Request) {
	p, ok := a.paintingFromPath(w, r)
	if !ok {
		return
	}
	yearText := ""
	if p.Year != 0 {
		yearText = strconv.Itoa(p.Year)
	}
	a.renderForm(w, r, http.StatusOK, formView{
		Painting: &p,
		Input: gallery.PaintingInput{
			Title: p.Title, Technique: p.Technique, Size: p.Size, Year: p.Year,
			Description: p.Description, Visible: p.Visible,
		},
		YearText: yearText,
		Crop:     editCrop(p, ""),
	})
}

func (a *Admin) update(w http.ResponseWriter, r *http.Request) {
	p, ok := a.paintingFromPath(w, r)
	if !ok {
		return
	}
	in, yearText, errs := parsePaintingForm(r)
	merge(errs, in.Validate(a.now()))
	data, err := readPhoto(r)
	if err != nil {
		errs["photo"] = photoError(err)
		if errs["photo"] == "" {
			errs["photo"] = msgPhotoUnreadable
		}
	}
	if len(errs) == 0 {
		var photo *gallery.Photo
		switch {
		case data != nil:
			photo = &gallery.Photo{Original: data, Crop: parseCrop(r)}
		case cropChanged(r):
			photo = &gallery.Photo{Crop: parseCrop(r)}
		}
		_, err := a.g.UpdatePainting(r.Context(), p.ID, in, photo)
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
	} else if _, bad := errs["photo"]; data != nil && !bad {
		errs["photo"] = msgPhotoAgain
	}
	a.renderForm(w, r, http.StatusUnprocessableEntity, formView{
		Painting: &p, Input: in, YearText: yearText, Errors: errs, Crop: editCrop(p, errs["photo"]),
	})
}

func (a *Admin) paintingOriginal(w http.ResponseWriter, r *http.Request) {
	p, ok := a.paintingFromPath(w, r)
	if !ok {
		return
	}
	a.serveOriginal(w, r, images.PaintingDir(p.ID))
}

// serveOriginal sends a stored original to the logged-in admin for re-cropping.
func (a *Admin) serveOriginal(w http.ResponseWriter, r *http.Request, dir string) {
	data, err := a.g.Disk.LoadOriginal(dir)
	if errors.Is(err, os.ErrNotExist) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	noCache(w)
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Write(data)
}

func (a *Admin) deleteConfirm(w http.ResponseWriter, r *http.Request) {
	p, ok := a.paintingFromPath(w, r)
	if !ok {
		return
	}
	a.render(w, r, http.StatusOK, "delete.html", view{Title: "Удалить картину", Nav: "paintings", Data: p})
}

func (a *Admin) deletePainting(w http.ResponseWriter, r *http.Request) {
	p, ok := a.paintingFromPath(w, r)
	if !ok {
		return
	}
	if err := a.g.DeletePainting(r.Context(), p.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/paintings?msg=deleted", http.StatusSeeOther)
}
