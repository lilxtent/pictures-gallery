// Package gallery implements the content-editing use cases: adding, changing
// and removing paintings and the author photo, keeping the database and the
// image files in step.
package gallery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

// ErrNoPhoto is returned when a new painting has no photo.
var ErrNoPhoto = errors.New("gallery: photo is required")

// Gallery ties the store and the image files together.
type Gallery struct {
	Store *store.Store
	Disk  *images.Disk
}

// PaintingInput holds the editable text fields of a painting.
type PaintingInput struct {
	Title       string
	Technique   string
	Size        string
	Year        int // 0 = not specified
	Description string
	Visible     bool
}

// FieldErrors maps form field names to Russian error messages.
type FieldErrors map[string]string

// Photo is an uploaded original plus how to crop it. A nil Original means
// "re-crop the original that is already stored".
type Photo struct {
	Original []byte
	Crop     images.Crop
}

// CleanText normalises line endings and trims surrounding whitespace.
func CleanText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

func (in PaintingInput) normalize() PaintingInput {
	in.Title = strings.TrimSpace(in.Title)
	in.Technique = strings.TrimSpace(in.Technique)
	in.Size = strings.TrimSpace(in.Size)
	in.Description = CleanText(in.Description)
	return in
}

// Validate checks the input and returns messages for invalid fields.
func (in PaintingInput) Validate(now time.Time) FieldErrors {
	errs := FieldErrors{}
	if strings.TrimSpace(in.Title) == "" {
		errs["title"] = "Укажите название"
	}
	if in.Year != 0 && (in.Year < 1900 || in.Year > now.Year()) {
		errs["year"] = fmt.Sprintf("Год должен быть от 1900 до %d", now.Year())
	}
	return errs
}

func apply(p *store.Painting, in PaintingInput) {
	p.Title = in.Title
	p.Technique = in.Technique
	p.Size = in.Size
	p.Year = in.Year
	p.Description = in.Description
	p.Visible = in.Visible
}

// AddPainting processes the photo, stores the painting at the top of the list
// and writes its image files.
func (g *Gallery) AddPainting(ctx context.Context, in PaintingInput, photo Photo) (store.Painting, error) {
	if len(photo.Original) == 0 {
		return store.Painting{}, ErrNoPhoto
	}
	res, crop, err := g.process("", photo)
	if err != nil {
		return store.Painting{}, err
	}
	var p store.Painting
	apply(&p, in.normalize())
	p.Crop, p.ImageVersion, p.ImageWidth, p.ImageHeight = crop, 1, res.Width, res.Height
	if err := g.Store.CreatePainting(ctx, &p); err != nil {
		return store.Painting{}, err
	}
	dir := images.PaintingDir(p.ID)
	if err := g.writeImages(dir, photo.Original, p.ImageVersion, res); err != nil {
		_ = g.Store.DeletePainting(ctx, p.ID)
		_ = g.Disk.RemoveDir(dir)
		return store.Painting{}, err
	}
	return p, nil
}

// UpdatePainting saves new text fields and, when photo is not nil, a new
// image version (from a new original or a re-crop of the stored one).
func (g *Gallery) UpdatePainting(ctx context.Context, id int64, in PaintingInput, photo *Photo) (store.Painting, error) {
	p, err := g.Store.GetPainting(ctx, id)
	if err != nil {
		return store.Painting{}, err
	}
	apply(&p, in.normalize())
	dir := images.PaintingDir(id)
	oldVersion := p.ImageVersion
	if photo != nil {
		res, crop, err := g.process(dir, *photo)
		if err != nil {
			return store.Painting{}, err
		}
		p.ImageVersion++
		p.Crop, p.ImageWidth, p.ImageHeight = crop, res.Width, res.Height
		if err := g.writeImages(dir, photo.Original, p.ImageVersion, res); err != nil {
			_ = g.Disk.RemoveVariants(dir, p.ImageVersion)
			return store.Painting{}, err
		}
	}
	if err := g.Store.UpdatePainting(ctx, &p); err != nil {
		if photo != nil {
			_ = g.Disk.RemoveVariants(dir, p.ImageVersion)
		}
		return store.Painting{}, err
	}
	if photo != nil {
		_ = g.Disk.RemoveVariants(dir, oldVersion)
	}
	return p, nil
}

// DeletePainting removes the painting and all its image files.
func (g *Gallery) DeletePainting(ctx context.Context, id int64) error {
	if err := g.Store.DeletePainting(ctx, id); err != nil {
		return err
	}
	return g.Disk.RemoveDir(images.PaintingDir(id))
}

// SetAboutPhoto stores a new author photo or re-crops the existing one.
func (g *Gallery) SetAboutPhoto(ctx context.Context, photo Photo) error {
	info, err := site.Load(ctx, g.Store)
	if err != nil {
		return err
	}
	old := 0
	if info.AboutPhoto != nil {
		old = info.AboutPhoto.Version
	}
	res, crop, err := g.process(images.AboutDir, photo)
	if err != nil {
		return err
	}
	version := old + 1
	if err := g.writeImages(images.AboutDir, photo.Original, version, res); err != nil {
		return err
	}
	raw, err := json.Marshal(site.Photo{Version: version, Width: res.Width, Height: res.Height, Crop: crop})
	if err != nil {
		return err
	}
	if err := g.Store.SetSettings(ctx, map[string]string{site.KeyAboutPhoto: string(raw)}); err != nil {
		_ = g.Disk.RemoveVariants(images.AboutDir, version)
		return err
	}
	if old > 0 {
		_ = g.Disk.RemoveVariants(images.AboutDir, old)
	}
	return nil
}

// process loads the original when needed and produces the variants.
func (g *Gallery) process(dir string, photo Photo) (images.Result, images.Crop, error) {
	original := photo.Original
	if len(original) == 0 {
		var err error
		if original, err = g.Disk.LoadOriginal(dir); err != nil {
			return images.Result{}, images.Crop{}, err
		}
	} else if _, err := images.DetectExt(original); err != nil {
		return images.Result{}, images.Crop{}, err
	}
	crop := photo.Crop
	crop.Rotation = images.NormalizeRotation(crop.Rotation)
	res, err := images.Process(original, crop)
	return res, crop, err
}

func (g *Gallery) writeImages(dir string, original []byte, version int, res images.Result) error {
	if len(original) > 0 {
		if err := g.Disk.SaveOriginal(dir, original); err != nil {
			return err
		}
	}
	return g.Disk.WriteVariants(dir, version, res.Variants)
}
