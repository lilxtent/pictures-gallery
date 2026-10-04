// Package testutil holds helpers shared by tests in several packages.
package testutil

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
)

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return img
}

// JPEG returns a w×h single-colour JPEG.
func JPEG(t testing.TB, w, h int, c color.Color) []byte {
	t.Helper()
	return encodeJPEG(t, solid(w, h, c))
}

// SplitJPEG returns a w×h JPEG whose left half is left and right half is right.
func SplitJPEG(t testing.TB, w, h int, left, right color.Color) []byte {
	t.Helper()
	img := solid(w, h, left)
	draw.Draw(img, image.Rect(w/2, 0, w, h), &image.Uniform{C: right}, image.Point{}, draw.Src)
	return encodeJPEG(t, img)
}

// PNG returns a w×h single-colour PNG.
func PNG(t testing.TB, w, h int, c color.Color) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(w, h, c)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// WithOrientation inserts a minimal EXIF segment carrying the given
// orientation tag right after the JPEG SOI marker, like a phone camera does.
func WithOrientation(t testing.TB, jpg []byte, orientation uint16) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("WithOrientation: not a JPEG")
	}
	var tiff bytes.Buffer
	tiff.WriteString("MM")
	binary.Write(&tiff, binary.BigEndian, uint16(0x002A))
	binary.Write(&tiff, binary.BigEndian, uint32(8))      // offset of IFD0
	binary.Write(&tiff, binary.BigEndian, uint16(1))      // one entry
	binary.Write(&tiff, binary.BigEndian, uint16(0x0112)) // Orientation
	binary.Write(&tiff, binary.BigEndian, uint16(3))      // SHORT
	binary.Write(&tiff, binary.BigEndian, uint32(1))      // count
	binary.Write(&tiff, binary.BigEndian, orientation)
	binary.Write(&tiff, binary.BigEndian, uint16(0)) // padding
	binary.Write(&tiff, binary.BigEndian, uint32(0)) // no next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var seg bytes.Buffer
	seg.Write([]byte{0xFF, 0xE1})
	binary.Write(&seg, binary.BigEndian, uint16(len(payload)+2))
	seg.Write(payload)

	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg.Bytes()...)
	return append(out, jpg[2:]...)
}
