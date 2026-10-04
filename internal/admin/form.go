package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
)

const (
	msgPhotoTooBig     = "Фото слишком большое (максимум 30 МБ)"
	msgPhotoUnreadable = "Не удалось прочитать фото. Попробуйте другой файл"
	msgPhotoMissing    = "Выберите фото"
	msgPhotoAgain      = "После ошибки фото нужно выбрать ещё раз"
)

var errPhotoTooBig = errors.New("admin: photo too big")

// cropField is the data for the "crop" partial.
type cropField struct {
	CurrentURL  string // preview of the current image, "" if none
	OriginalURL string // where the stored original can be loaded for re-cropping, "" if none
	CropJSON    string // previous crop in Cropper.js format
	Error       string
	Required    bool // a photo must be chosen before saving
}

func parsePaintingForm(r *http.Request) (gallery.PaintingInput, string, gallery.FieldErrors) {
	errs := gallery.FieldErrors{}
	yearText := strings.TrimSpace(r.PostFormValue("year"))
	year := 0
	if yearText != "" {
		y, err := strconv.Atoi(yearText)
		if err != nil {
			errs["year"] = "Год должен быть числом"
		} else {
			year = y
		}
	}
	in := gallery.PaintingInput{
		Title:       r.PostFormValue("title"),
		Technique:   r.PostFormValue("technique"),
		Size:        r.PostFormValue("size"),
		Year:        year,
		Description: r.PostFormValue("description"),
		Visible:     r.PostFormValue("visible") == "on",
	}
	return in, yearText, errs
}

func parseCrop(r *http.Request) images.Crop {
	n := func(name string) int {
		v, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue(name)))
		return v
	}
	return images.Crop{X: n("crop_x"), Y: n("crop_y"), W: n("crop_w"), H: n("crop_h"), Rotation: n("crop_rotate")}
}

func cropChanged(r *http.Request) bool { return r.PostFormValue("crop_changed") == "1" }

// readPhoto returns the uploaded photo, or nil when no file was chosen.
func readPhoto(r *http.Request) ([]byte, error) {
	f, hdr, err := r.FormFile("photo")
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if hdr.Size > maxPhotoBytes {
		return nil, errPhotoTooBig
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	return data, nil
}

// photoError maps photo errors to a message, or "" for unexpected errors.
func photoError(err error) string {
	switch {
	case errors.Is(err, errPhotoTooBig):
		return msgPhotoTooBig
	case errors.Is(err, images.ErrDecode):
		return msgPhotoUnreadable
	}
	return ""
}

func merge(dst, src gallery.FieldErrors) {
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

// cropJSON encodes a crop in the shape Cropper.js accepts as its `data` option.
func cropJSON(c images.Crop) string {
	if c.W == 0 || c.H == 0 {
		return ""
	}
	b, _ := json.Marshal(map[string]int{"x": c.X, "y": c.Y, "width": c.W, "height": c.H, "rotate": c.Rotation})
	return string(b)
}
