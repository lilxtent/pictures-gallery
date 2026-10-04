// Package images turns uploaded photos into the resized JPEGs the site serves
// and manages where they live on disk.
package images

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"net/http"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register WebP decoding
)

// Sizes are the long-edge pixel sizes of the generated variants.
var Sizes = []int{600, 1200, 2000}

// ErrDecode means the uploaded bytes are not a supported image.
var ErrDecode = errors.New("images: cannot decode photo")

// Crop describes how to cut the painting out of the photo. Rotation is in
// clockwise degrees and is applied first; X/Y/W/H are pixels in the rotated
// image. W or H of zero means "use the whole image".
type Crop struct {
	X        int `json:"x"`
	Y        int `json:"y"`
	W        int `json:"w"`
	H        int `json:"h"`
	Rotation int `json:"rotation"`
}

// Result holds encoded JPEG variants keyed by size, and the pixel size of the
// cropped image they were made from.
type Result struct {
	Variants      map[int][]byte
	Width, Height int
}

// Process decodes the original, applies EXIF orientation, rotation and crop,
// and encodes one JPEG per entry in Sizes. Re-encoding drops all metadata.
func Process(original []byte, c Crop) (Result, error) {
	img, err := imaging.Decode(bytes.NewReader(original), imaging.AutoOrientation(true))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	img = rotate(img, c.Rotation)
	if c.W > 0 && c.H > 0 {
		rect := image.Rect(c.X, c.Y, c.X+c.W, c.Y+c.H).Intersect(img.Bounds())
		if !rect.Empty() {
			img = imaging.Crop(img, rect)
		}
	}
	b := img.Bounds()
	res := Result{Variants: make(map[int][]byte, len(Sizes)), Width: b.Dx(), Height: b.Dy()}
	for _, s := range Sizes {
		var buf bytes.Buffer
		resized := imaging.Fit(img, s, s, imaging.Lanczos)
		if err := imaging.Encode(&buf, resized, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
			return Result{}, fmt.Errorf("images: encode %d: %w", s, err)
		}
		res.Variants[s] = buf.Bytes()
	}
	return res, nil
}

// NormalizeRotation maps any angle to the nearest of 0, 90, 180, 270.
func NormalizeRotation(deg int) int {
	r := ((deg % 360) + 360) % 360
	return (r + 45) / 90 * 90 % 360
}

func rotate(img image.Image, deg int) image.Image {
	// imaging rotates counter-clockwise, our angles are clockwise.
	switch NormalizeRotation(deg) {
	case 90:
		return imaging.Rotate270(img)
	case 180:
		return imaging.Rotate180(img)
	case 270:
		return imaging.Rotate90(img)
	}
	return img
}

// DetectExt returns the file extension for a supported upload format.
func DetectExt(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	}
	return "", ErrDecode
}
