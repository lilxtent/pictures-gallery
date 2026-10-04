// Package images turns uploaded photos into the resized JPEGs the site serves
// and manages where they live on disk.
package images

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"net/http"
	"sort"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register WebP decoding
)

// Sizes are the long-edge pixel sizes of the generated variants.
var Sizes = []int{600, 1200, 2000}

// ErrDecode means the uploaded bytes are not a supported image.
var ErrDecode = errors.New("images: cannot decode photo")

// ErrTooManyPixels means the photo's resolution exceeds MaxPixels. Decoding
// such an image needs gigabytes of RAM, so it is refused before decoding.
var ErrTooManyPixels = errors.New("images: photo resolution is too high")

// MaxPixels is the largest accepted photo, in pixels (width*height). A 48 MP
// phone photo is fine; a decompression bomb is not.
const MaxPixels = 120_000_000

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
//
// To keep peak memory low on a small server it refuses photos over MaxPixels
// before decoding, and derives each smaller variant from the next larger one
// so the full-size pixels can be freed after the first resize.
func Process(original []byte, c Crop) (Result, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(original))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Result{}, fmt.Errorf("%w: %dx%d", ErrTooManyPixels, cfg.Width, cfg.Height)
	}
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
	res := Result{Width: b.Dx(), Height: b.Dy()}
	res.Variants, err = encodeVariants(img, res.Width, res.Height)
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// encodeVariants resizes from the largest size down, each step working from
// the previous (smaller) result. It takes ownership of img: once the first
// resize is done nothing refers to the full-size pixels any more.
func encodeVariants(img image.Image, w, h int) (map[int][]byte, error) {
	sizes := append([]int(nil), Sizes...)
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
	variants := make(map[int][]byte, len(sizes))
	cur := img
	img = nil
	for _, s := range sizes {
		// Target size comes from the original dimensions, not from the
		// previous variant, so rounding errors do not accumulate.
		tw, th := fitSize(w, h, s)
		if cb := cur.Bounds(); cb.Dx() != tw || cb.Dy() != th {
			cur = imaging.Resize(cur, tw, th, imaging.Lanczos)
		}
		var buf bytes.Buffer
		if err := imaging.Encode(&buf, cur, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
			return nil, fmt.Errorf("images: encode %d: %w", s, err)
		}
		variants[s] = buf.Bytes()
	}
	return variants, nil
}

// fitSize scales w×h down so the long edge is at most max, never up.
func fitSize(w, h, max int) (int, int) {
	long := w
	if h > long {
		long = h
	}
	if long <= max {
		return w, h
	}
	scale := func(v int) int {
		r := (int64(v)*int64(max) + int64(long)/2) / int64(long)
		if r < 1 {
			r = 1
		}
		return int(r)
	}
	return scale(w), scale(h)
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
