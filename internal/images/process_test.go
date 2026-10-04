package images

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

var (
	red  = color.RGBA{255, 0, 0, 255}
	blue = color.RGBA{0, 0, 255, 255}
)

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("variant is not a JPEG: %v", err)
	}
	return img
}

func size(t *testing.T, b []byte) (int, int) {
	t.Helper()
	r := decode(t, b).Bounds()
	return r.Dx(), r.Dy()
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r>>8 > 200 && g>>8 < 60 && b>>8 < 60
}

func isBlue(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r>>8 < 60 && g>>8 < 60 && b>>8 > 200
}

func TestProcessProducesAllSizes(t *testing.T) {
	res, err := Process(testutil.JPEG(t, 3000, 2000, red), Crop{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 3000 || res.Height != 2000 {
		t.Fatalf("Result size = %dx%d, want 3000x2000", res.Width, res.Height)
	}
	want := map[int][2]int{600: {600, 400}, 1200: {1200, 800}, 2000: {2000, 1333}}
	for s, wh := range want {
		w, h := size(t, res.Variants[s])
		if w != wh[0] || h != wh[1] {
			t.Errorf("variant %d = %dx%d, want %dx%d", s, w, h, wh[0], wh[1])
		}
	}
}

func TestProcessDoesNotUpscale(t *testing.T) {
	res, err := Process(testutil.JPEG(t, 300, 200, red), Crop{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range Sizes {
		if w, h := size(t, res.Variants[s]); w != 300 || h != 200 {
			t.Errorf("variant %d = %dx%d, want 300x200", s, w, h)
		}
	}
}

func TestProcessCrop(t *testing.T) {
	src := testutil.SplitJPEG(t, 100, 100, red, blue)
	res, err := Process(src, Crop{X: 50, Y: 0, W: 50, H: 100})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 50 || res.Height != 100 {
		t.Fatalf("cropped size = %dx%d, want 50x100", res.Width, res.Height)
	}
	if c := decode(t, res.Variants[600]).At(25, 50); !isBlue(c) {
		t.Fatalf("centre of crop = %v, want blue", c)
	}
}

func TestProcessCropIsClampedToImage(t *testing.T) {
	res, err := Process(testutil.JPEG(t, 100, 100, red), Crop{X: 80, Y: 80, W: 50, H: 50})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 20 || res.Height != 20 {
		t.Fatalf("size = %dx%d, want 20x20", res.Width, res.Height)
	}
}

func TestProcessCropOutsideImageUsesWholeImage(t *testing.T) {
	res, err := Process(testutil.JPEG(t, 100, 60, red), Crop{X: 500, Y: 500, W: 10, H: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 100 || res.Height != 60 {
		t.Fatalf("size = %dx%d, want 100x60", res.Width, res.Height)
	}
}

func TestProcessRotatesClockwise(t *testing.T) {
	src := testutil.SplitJPEG(t, 40, 20, red, blue) // red left, blue right
	res, err := Process(src, Crop{Rotation: 90})
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, res.Variants[600])
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 40 {
		t.Fatalf("rotated size = %dx%d, want 20x40", b.Dx(), b.Dy())
	}
	// Clockwise: the left edge becomes the top.
	if !isRed(img.At(10, 5)) || !isBlue(img.At(10, 35)) {
		t.Fatalf("top=%v bottom=%v, want red on top and blue at bottom", img.At(10, 5), img.At(10, 35))
	}
}

func TestProcessRotatesBeforeCropping(t *testing.T) {
	src := testutil.SplitJPEG(t, 40, 20, red, blue)
	res, err := Process(src, Crop{X: 0, Y: 0, W: 20, H: 20, Rotation: 90})
	if err != nil {
		t.Fatal(err)
	}
	if c := decode(t, res.Variants[600]).At(10, 10); !isRed(c) {
		t.Fatalf("top square after rotation = %v, want red", c)
	}
}

func TestProcessAppliesEXIFOrientation(t *testing.T) {
	src := testutil.WithOrientation(t, testutil.JPEG(t, 40, 20, red), 6) // 6 = rotate 90° CW to display
	res, err := Process(src, Crop{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 20 || res.Height != 40 {
		t.Fatalf("size = %dx%d, want 20x40", res.Width, res.Height)
	}
}

func TestProcessStripsMetadata(t *testing.T) {
	src := testutil.WithOrientation(t, testutil.JPEG(t, 40, 20, red), 1)
	if !bytes.Contains(src, []byte("Exif")) {
		t.Fatal("test input should contain EXIF")
	}
	res, err := Process(src, Crop{})
	if err != nil {
		t.Fatal(err)
	}
	for s, b := range res.Variants {
		if bytes.Contains(b, []byte("Exif")) {
			t.Errorf("variant %d still contains EXIF", s)
		}
	}
}

func TestProcessAcceptsPNG(t *testing.T) {
	res, err := Process(testutil.PNG(t, 50, 40, red), Crop{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 50 || res.Height != 40 {
		t.Fatalf("size = %dx%d, want 50x40", res.Width, res.Height)
	}
}

func TestProcessRejectsGarbage(t *testing.T) {
	_, err := Process([]byte("definitely not an image"), Crop{})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
}

func TestProcessRejectsTooManyPixels(t *testing.T) {
	// 20000x20000 = 400 MP, declared in the header only.
	_, err := Process(testutil.PNGHeaderOnly(20000, 20000), Crop{})
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("err = %v, want ErrTooManyPixels", err)
	}
	if errors.Is(err, ErrDecode) {
		t.Fatalf("too many pixels must be distinguishable from ErrDecode: %v", err)
	}
}

func TestProcessAcceptsTheMegapixelLimit(t *testing.T) {
	// Exactly at the limit passes the check; the header has no pixel data so
	// decoding then fails with ErrDecode, not ErrTooManyPixels.
	_, err := Process(testutil.PNGHeaderOnly(12000, 10000), Crop{})
	if errors.Is(err, ErrTooManyPixels) || !errors.Is(err, ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
}

func TestNormalizeRotation(t *testing.T) {
	for in, want := range map[int]int{0: 0, 90: 90, 180: 180, 270: 270, 360: 0, -90: 270, 450: 90, 89: 90, -180: 180} {
		if got := NormalizeRotation(in); got != want {
			t.Errorf("NormalizeRotation(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDetectExt(t *testing.T) {
	if ext, err := DetectExt(testutil.JPEG(t, 2, 2, red)); err != nil || ext != ".jpg" {
		t.Errorf("JPEG: got %q, %v", ext, err)
	}
	if ext, err := DetectExt(testutil.PNG(t, 2, 2, red)); err != nil || ext != ".png" {
		t.Errorf("PNG: got %q, %v", ext, err)
	}
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 16)...)
	if ext, err := DetectExt(webp); err != nil || ext != ".webp" {
		t.Errorf("WebP: got %q, %v", ext, err)
	}
	if _, err := DetectExt([]byte("GIF89a......")); !errors.Is(err, ErrDecode) {
		t.Errorf("GIF: err = %v, want ErrDecode", err)
	}
}
