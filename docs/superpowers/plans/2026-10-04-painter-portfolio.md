# Painter Portfolio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Russian-language portfolio site for a watercolour painter, with a phone-friendly admin where she uploads, crops, describes and orders her paintings herself.

**Architecture:** One Go binary serves the public site (server-rendered `html/template`) and the admin under `/admin`. Content lives in SQLite; photos are processed on upload into three JPEG sizes stored on disk next to the private original. A small `gallery` package orchestrates store + image processing so HTTP handlers stay thin. Deployed with Docker Compose + Caddy on a Russian VPS.

**Tech Stack:** Go (stdlib `net/http`, `html/template`, `log/slog`, `embed`), `modernc.org/sqlite`, `github.com/disintegration/imaging`, `golang.org/x/image/webp`, `golang.org/x/crypto/bcrypt`; vendored Cropper.js 1.6.2 and SortableJS 1.15.2; self-hosted Lora + Marck Script fonts; Docker, Caddy 2, rclone.

**Spec:** `docs/superpowers/specs/2026-10-04-painter-portfolio-design.md`

## Global Constraints

- Module path: `github.com/lilxtent/pictures-gallery`. `go.mod` minimum `go 1.24` (needed for `slog.DiscardHandler`); local toolchain is Go 1.27.
- Pure Go only, `CGO_ENABLED=0` must build.
- All user-facing text (public site and admin) is in Russian. Code, comments, commit messages in English.
- No external CDNs at runtime: fonts and JS libraries are vendored under `internal/assets/static/`.
- No cookies or analytics on the public site. Only the admin sets a cookie.
- Upload limit: 30 MB per photo (`maxPhotoBytes = 30 << 20`); request limit 32 MB.
- Image variant sizes (long edge, px): 600, 1200, 2000. JPEG quality 85. Never upscale.
- Public variant URL format: `/media/{dir}/v{version}-{size}.jpg`, where `dir` is `paintings/{id}` or `about`. Originals are never served publicly.
- Rotation is clockwise degrees, normalized to 0/90/180/270. Rotation is applied **before** the crop; crop coordinates are in the rotated image's pixel space (this matches Cropper.js `getData()`).
- Session cookie: name `gallery_session`, path `/admin`, `HttpOnly`, `SameSite=Lax`, `Secure` unless `DEV=1`, lifetime 30 days. Only SHA-256 of the token is stored.
- Login rate limit: 5 failures per IP per sliding 15 minutes.
- Visual style "Watercolour paper": background `#efe8dc`, mat `#fdfbf7`, ink `#3b332b`, muted `#8b7a66`, accent `#9c4a2f`, dashed lines `#cdbfa9`; name in Marck Script, text in Lora.
- Commit after every task (frequent small commits are fine). Run `gofmt -l .` (expect no output) and `go vet ./...` before each commit.

## File Structure

```
go.mod, go.sum
.github/workflows/test.yml          CI: vet + test
cmd/gallery/
  main.go                           config, wiring, server, `backup` subcommand
  middleware.go, middleware_test.go request logging + panic recovery
internal/slug/                      Russian → Latin slugs
internal/testutil/                  JPEG/PNG/EXIF generators for tests
internal/images/
  process.go                        decode, orient, rotate, crop, resize, encode
  disk.go                           on-disk layout of originals/variants, URLs
internal/store/
  store.go                          Open, migrations, Backup
  migrations/001_init.sql
  paintings.go, settings.go, sessions.go
internal/site/                      typed view of settings: name, texts, contacts, about photo
internal/gallery/                   use cases: add/update/delete painting, about photo, validation
internal/seed/                      DEV sample content (+ photos/*.jpg)
internal/assets/                    embedded static files served at /static/
  static/site.css, site.js, admin.css, admin.js, fonts/, vendor/
internal/web/                       public handlers + templates
internal/admin/                     admin handlers, auth, templates
deploy/                             Dockerfile, docker-compose.yml, Caddyfile, backup.sh, deploy.sh, .env.example
README.md
```

Dependency direction (no cycles): `slug`, `images` ← `store` ← `site` ← `gallery` ← `admin`, `seed`; `web` uses `store`, `images`, `site`, `assets`; `admin` also uses `assets` (via the shared `/static/` route registered by `web`).

---

### Task 1: Go module, slug package, CI

**Files:**
- Create: `go.mod`
- Create: `internal/slug/slug.go`
- Test: `internal/slug/slug_test.go`
- Create: `.github/workflows/test.yml`

**Interfaces:**
- Produces: `slug.Make(title string) string` — lowercase Latin letters, digits, single dashes; `""` when nothing usable.

- [ ] **Step 1: Initialise the module**

```bash
cd /Users/aleksandr/Projects/pictures-gallery
go mod init github.com/lilxtent/pictures-gallery
go mod edit -go=1.24
```

- [ ] **Step 2: Write the failing test**

`internal/slug/slug_test.go`:
```go
package slug

import (
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Жёлтая лилия", "zheltaya-liliya"},
		{"Карпы кои", "karpy-koi"},
		{"Пион", "pion"},
		{"  Три   мака!!! ", "tri-maka"},
		{"Объявление", "obyavlenie"},
		{"Щука и ёж", "shchuka-i-ezh"},
		{"Зима 2024", "zima-2024"},
		{"Summer Sea", "summer-sea"},
		{"Ночь—день", "noch-den"},
		{"Й", "y"},
		{"!!!", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Make(tt.in); got != tt.want {
			t.Errorf("Make(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMakeTruncatesLongTitles(t *testing.T) {
	got := Make(strings.Repeat("а", 100))
	if got != strings.Repeat("a", 80) {
		t.Fatalf("got %q (len %d), want 80 a's", got, len(got))
	}
}

func TestMakeTruncationDoesNotLeaveTrailingDash(t *testing.T) {
	got := Make(strings.Repeat("a", 79) + " b")
	if strings.HasSuffix(got, "-") {
		t.Fatalf("got %q, must not end with a dash", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/slug/`
Expected: FAIL — `undefined: Make`

- [ ] **Step 4: Write the implementation**

`internal/slug/slug.go`:
```go
// Package slug turns Russian painting titles into URL-safe Latin slugs.
package slug

import "strings"

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
}

const maxLen = 80

// Make returns a lowercase slug made of Latin letters, digits and single dashes.
// It returns "" when the title contains no letters or digits.
func Make(title string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(title) {
		var part string
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			part = string(r)
		} else if t, ok := translit[r]; ok {
			part = t
		} else {
			pendingDash = b.Len() > 0
			continue
		}
		if part == "" {
			continue
		}
		if pendingDash {
			b.WriteByte('-')
			pendingDash = false
		}
		b.WriteString(part)
	}
	s := b.String()
	if len(s) > maxLen {
		s = strings.TrimRight(s[:maxLen], "-")
	}
	return s
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/slug/`
Expected: `ok`

- [ ] **Step 6: Add CI**

`.github/workflows/test.yml`:
```yaml
name: test
on:
  push:
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go test ./...
```

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod internal/slug .github
git commit -m "feat: add slug transliteration and CI"
```

---

### Task 2: Image processing

**Files:**
- Create: `internal/testutil/testutil.go`
- Create: `internal/images/process.go`
- Test: `internal/images/process_test.go`

**Interfaces:**
- Produces (testutil, used by tests in later tasks):
  - `testutil.JPEG(t testing.TB, w, h int, c color.Color) []byte`
  - `testutil.SplitJPEG(t testing.TB, w, h int, left, right color.Color) []byte` — left half one colour, right half another
  - `testutil.PNG(t testing.TB, w, h int, c color.Color) []byte`
  - `testutil.WithOrientation(t testing.TB, jpg []byte, orientation uint16) []byte` — inserts an EXIF APP1 segment
- Produces (images):
  - `type Crop struct { X, Y, W, H, Rotation int }` (JSON tags `x,y,w,h,rotation`); zero `W`/`H` means "whole image"
  - `var Sizes = []int{600, 1200, 2000}`
  - `var ErrDecode error`
  - `type Result struct { Variants map[int][]byte; Width, Height int }` — `Width/Height` are of the cropped image before resizing
  - `func Process(original []byte, c Crop) (Result, error)`
  - `func NormalizeRotation(deg int) int`
  - `func DetectExt(data []byte) (string, error)` — `.jpg`, `.png`, `.webp`, else `ErrDecode`

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/disintegration/imaging@v1.6.2 golang.org/x/image@latest
```

- [ ] **Step 2: Write the test helpers**

`internal/testutil/testutil.go`:
```go
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
	binary.Write(&tiff, binary.BigEndian, uint16(0))      // padding
	binary.Write(&tiff, binary.BigEndian, uint32(0))      // no next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var seg bytes.Buffer
	seg.Write([]byte{0xFF, 0xE1})
	binary.Write(&seg, binary.BigEndian, uint16(len(payload)+2))
	seg.Write(payload)

	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg.Bytes()...)
	return append(out, jpg[2:]...)
}
```

- [ ] **Step 3: Write the failing tests**

`internal/images/process_test.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/images/`
Expected: FAIL — `undefined: Process` (and others)

- [ ] **Step 5: Write the implementation**

`internal/images/process.go`:
```go
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
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go mod tidy && go test ./internal/images/`
Expected: `ok`

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod go.sum internal/testutil internal/images
git commit -m "feat: add photo processing (orient, rotate, crop, resize)"
```

---

### Task 3: Image storage on disk

**Files:**
- Create: `internal/images/disk.go`
- Test: `internal/images/disk_test.go`

**Interfaces:**
- Consumes: `DetectExt`, `Sizes` from Task 2.
- Produces:
  - `type Disk struct { Root string }`
  - `const AboutDir = "about"`
  - `func PaintingDir(id int64) string` → `"paintings/{id}"`
  - `func URL(dir string, version, size int) string` → `"/media/{dir}/v{version}-{size}.jpg"`
  - `func SrcSet(dir string, version int) string` → `"<url> 600w, <url> 1200w, <url> 2000w"`
  - `func (d *Disk) SaveOriginal(dir string, data []byte) error` — replaces any previous original
  - `func (d *Disk) LoadOriginal(dir string) ([]byte, error)` — `os.ErrNotExist` when missing
  - `func (d *Disk) WriteVariants(dir string, version int, variants map[int][]byte) error`
  - `func (d *Disk) RemoveVariants(dir string, version int) error`
  - `func (d *Disk) RemoveDir(dir string) error`
  - `func (d *Disk) VariantFile(dir, name string) (string, bool)` — absolute path of a servable variant; false for anything else (originals, traversal)

- [ ] **Step 1: Write the failing tests**

`internal/images/disk_test.go`:
```go
package images

import (
	"bytes"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestURLAndSrcSet(t *testing.T) {
	if got := URL(PaintingDir(7), 3, 600); got != "/media/paintings/7/v3-600.jpg" {
		t.Errorf("URL = %q", got)
	}
	want := "/media/about/v2-600.jpg 600w, /media/about/v2-1200.jpg 1200w, /media/about/v2-2000.jpg 2000w"
	if got := SrcSet(AboutDir, 2); got != want {
		t.Errorf("SrcSet = %q, want %q", got, want)
	}
}

func TestOriginalRoundTripAndReplace(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	jpg := testutil.JPEG(t, 10, 10, color.White)
	if err := d.SaveOriginal("paintings/1", jpg); err != nil {
		t.Fatal(err)
	}
	got, err := d.LoadOriginal("paintings/1")
	if err != nil || !bytes.Equal(got, jpg) {
		t.Fatalf("LoadOriginal = %d bytes, %v", len(got), err)
	}

	pngData := testutil.PNG(t, 10, 10, color.White)
	if err := d.SaveOriginal("paintings/1", pngData); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Root, "paintings", "1", "original.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old original.jpg should be gone, stat err = %v", err)
	}
	got, _ = d.LoadOriginal("paintings/1")
	if !bytes.Equal(got, pngData) {
		t.Fatal("LoadOriginal should return the new PNG")
	}
}

func TestLoadOriginalMissing(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	if _, err := d.LoadOriginal("about"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}

func TestSaveOriginalRejectsUnknownFormat(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	if err := d.SaveOriginal("about", []byte("GIF89a....")); !errors.Is(err, ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
}

func TestInvalidDirsAreRejected(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	for _, dir := range []string{"../etc", "paintings/abc", "paintings/1/..", "", "/abs"} {
		if err := d.SaveOriginal(dir, testutil.JPEG(t, 2, 2, color.White)); err == nil {
			t.Errorf("SaveOriginal(%q) should fail", dir)
		}
	}
}

func TestVariantsWriteServeRemove(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	variants := map[int][]byte{600: []byte("a"), 1200: []byte("b"), 2000: []byte("c")}
	if err := d.WriteVariants("paintings/5", 2, variants); err != nil {
		t.Fatal(err)
	}
	p, ok := d.VariantFile("paintings/5", "v2-1200.jpg")
	if !ok {
		t.Fatal("VariantFile should accept v2-1200.jpg")
	}
	if b, _ := os.ReadFile(p); string(b) != "b" {
		t.Fatalf("variant content = %q", b)
	}
	if err := d.RemoveVariants("paintings/5", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("variant should be removed")
	}
}

func TestVariantFileRejectsNonVariants(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	cases := [][2]string{
		{"paintings/5", "original.jpg"},
		{"paintings/5", "../../gallery.db"},
		{"paintings/5", "v1-600.png"},
		{"../x", "v1-600.jpg"},
	}
	for _, c := range cases {
		if _, ok := d.VariantFile(c[0], c[1]); ok {
			t.Errorf("VariantFile(%q, %q) should be rejected", c[0], c[1])
		}
	}
}

func TestRemoveDir(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	if err := d.SaveOriginal("paintings/9", testutil.JPEG(t, 2, 2, color.White)); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveDir("paintings/9"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Root, "paintings", "9")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dir should be removed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/images/`
Expected: FAIL — `undefined: Disk` (and others)

- [ ] **Step 3: Write the implementation**

`internal/images/disk.go`:
```go
package images

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AboutDir is the storage directory of the author's photo.
const AboutDir = "about"

// PaintingDir is the storage directory of one painting's images.
func PaintingDir(id int64) string { return fmt.Sprintf("paintings/%d", id) }

// URL is the public address of one variant.
func URL(dir string, version, size int) string {
	return fmt.Sprintf("/media/%s/v%d-%d.jpg", dir, version, size)
}

// SrcSet builds an <img srcset> value covering all variant sizes.
func SrcSet(dir string, version int) string {
	parts := make([]string, len(Sizes))
	for i, s := range Sizes {
		parts[i] = fmt.Sprintf("%s %dw", URL(dir, version, s), s)
	}
	return strings.Join(parts, ", ")
}

var (
	validDir    = regexp.MustCompile(`^(paintings/[0-9]+|about)$`)
	variantName = regexp.MustCompile(`^v[0-9]+-[0-9]+\.jpg$`)
)

// Disk stores originals and variants under Root:
//
//	{Root}/{dir}/original.{ext}       private, never served
//	{Root}/{dir}/v{version}-{size}.jpg
type Disk struct {
	Root string
}

func (d *Disk) path(dir string) (string, error) {
	if !validDir.MatchString(dir) {
		return "", fmt.Errorf("images: invalid dir %q", dir)
	}
	return filepath.Join(d.Root, filepath.FromSlash(dir)), nil
}

// SaveOriginal stores the uploaded file, replacing any previous original.
func (d *Disk) SaveOriginal(dir string, data []byte) error {
	ext, err := DetectExt(data)
	if err != nil {
		return err
	}
	p, err := d.path(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(p, "upload.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(p, "original.*"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return os.Rename(tmp, filepath.Join(p, "original"+ext))
}

// LoadOriginal returns the stored original or an error wrapping os.ErrNotExist.
func (d *Disk) LoadOriginal(dir string) ([]byte, error) {
	p, err := d.path(dir)
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(p, "original.*"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("images: no original in %s: %w", dir, os.ErrNotExist)
	}
	return os.ReadFile(matches[0])
}

// WriteVariants writes v{version}-{size}.jpg for each variant.
func (d *Disk) WriteVariants(dir string, version int, variants map[int][]byte) error {
	p, err := d.path(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	for size, data := range variants {
		name := filepath.Join(p, fmt.Sprintf("v%d-%d.jpg", version, size))
		if err := os.WriteFile(name, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// RemoveVariants deletes every variant of the given version.
func (d *Disk) RemoveVariants(dir string, version int) error {
	p, err := d.path(dir)
	if err != nil {
		return err
	}
	matches, _ := filepath.Glob(filepath.Join(p, fmt.Sprintf("v%d-*.jpg", version)))
	for _, f := range matches {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}

// RemoveDir deletes the directory with the original and all variants.
func (d *Disk) RemoveDir(dir string) error {
	p, err := d.path(dir)
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}

// VariantFile returns the path of a publicly servable variant file.
func (d *Disk) VariantFile(dir, name string) (string, bool) {
	p, err := d.path(dir)
	if err != nil || !variantName.MatchString(name) {
		return "", false
	}
	return filepath.Join(p, name), true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/images/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/images
git commit -m "feat: add on-disk storage for originals and variants"
```

---

### Task 4: Store — database, migrations, paintings

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/migrations/001_init.sql`
- Create: `internal/store/paintings.go`
- Test: `internal/store/paintings_test.go`

**Interfaces:**
- Consumes: `slug.Make` (Task 1), `images.Crop` (Task 2).
- Produces:
  - `var ErrNotFound error`
  - `type Store struct` with `func Open(path string) (*Store, error)`, `func (s *Store) Close() error`
  - `type Painting struct { ID int64; Slug, Title, Technique, Size string; Year int /*0 = none*/; Description string; Visible bool; Position int; Crop images.Crop; ImageVersion, ImageWidth, ImageHeight int; CreatedAt, UpdatedAt time.Time }`
  - `func (s *Store) CreatePainting(ctx, p *Painting) error` — sets `ID`, `Slug` (unique), `Position` (top), timestamps
  - `func (s *Store) UpdatePainting(ctx, p *Painting) error` — everything except `Slug`, `Position`, `CreatedAt`; `ErrNotFound` if missing
  - `func (s *Store) GetPainting(ctx, id int64) (Painting, error)`
  - `func (s *Store) GetPaintingBySlug(ctx, slug string) (Painting, error)`
  - `func (s *Store) ListPaintings(ctx, visibleOnly bool) ([]Painting, error)` — ordered by position, then id
  - `func (s *Store) ReorderPaintings(ctx, ids []int64) error` — position = index in `ids`
  - `func (s *Store) DeletePainting(ctx, id int64) error`

- [ ] **Step 1: Add the SQLite driver**

```bash
go get modernc.org/sqlite@latest
```

- [ ] **Step 2: Write the failing tests**

`internal/store/paintings_test.go`:
```go
package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/images"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func create(t *testing.T, s *Store, title string, visible bool) Painting {
	t.Helper()
	p := Painting{Title: title, Visible: visible, ImageVersion: 1, ImageWidth: 300, ImageHeight: 200}
	if err := s.CreatePainting(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func titles(ps []Painting) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Title
	}
	return out
}

func TestCreateAssignsIDSlugAndTopPosition(t *testing.T) {
	s := openTest(t)
	a := create(t, s, "Карпы кои", true)
	b := create(t, s, "Пион", true)
	if a.ID == 0 || a.Slug != "karpy-koi" || b.Slug != "pion" {
		t.Fatalf("got a=%+v b=%+v", a, b)
	}
	if b.Position >= a.Position {
		t.Fatalf("newer painting must be on top: a.Position=%d b.Position=%d", a.Position, b.Position)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be set")
	}
}

func TestSlugCollisionsGetSuffix(t *testing.T) {
	s := openTest(t)
	create(t, s, "Пион", true)
	second := create(t, s, "Пион", true)
	third := create(t, s, "пион!", true)
	if second.Slug != "pion-2" || third.Slug != "pion-3" {
		t.Fatalf("slugs = %q, %q", second.Slug, third.Slug)
	}
}

func TestEmptySlugFallsBack(t *testing.T) {
	s := openTest(t)
	a := create(t, s, "!!!", true)
	b := create(t, s, "???", true)
	if a.Slug != "kartina" || b.Slug != "kartina-2" {
		t.Fatalf("slugs = %q, %q", a.Slug, b.Slug)
	}
}

func TestGetPaintingRoundTrip(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := Painting{
		Title: "Пион", Technique: "Бумага, акварель", Size: "30×40 см", Year: 2025,
		Description: "Абзац", Visible: true,
		Crop:         images.Crop{X: 1, Y: 2, W: 3, H: 4, Rotation: 90},
		ImageVersion: 2, ImageWidth: 300, ImageHeight: 400,
	}
	if err := s.CreatePainting(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPainting(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != p.Title || got.Technique != p.Technique || got.Size != p.Size || got.Year != 2025 ||
		got.Description != p.Description || !got.Visible || got.Crop != p.Crop ||
		got.ImageVersion != 2 || got.ImageWidth != 300 || got.ImageHeight != 400 {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, p)
	}
	bySlug, err := s.GetPaintingBySlug(ctx, "pion")
	if err != nil || bySlug.ID != p.ID {
		t.Fatalf("GetPaintingBySlug = %+v, %v", bySlug, err)
	}
}

func TestYearZeroMeansNotSpecified(t *testing.T) {
	s := openTest(t)
	p := create(t, s, "Без года", true)
	got, _ := s.GetPainting(context.Background(), p.ID)
	if got.Year != 0 {
		t.Fatalf("Year = %d, want 0", got.Year)
	}
	var isNull bool
	s.db.QueryRow("SELECT year IS NULL FROM paintings WHERE id = ?", p.ID).Scan(&isNull)
	if !isNull {
		t.Fatal("year 0 must be stored as NULL")
	}
}

func TestNotFound(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.GetPainting(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPainting err = %v", err)
	}
	if _, err := s.GetPaintingBySlug(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPaintingBySlug err = %v", err)
	}
	if err := s.UpdatePainting(ctx, &Painting{ID: 42, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdatePainting err = %v", err)
	}
	if err := s.DeletePainting(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeletePainting err = %v", err)
	}
}

func TestUpdateKeepsSlugAndPosition(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := create(t, s, "Пион", true)
	p.Title = "Розовый пион"
	p.Visible = false
	p.ImageVersion = 3
	if err := s.UpdatePainting(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPainting(ctx, p.ID)
	if got.Title != "Розовый пион" || got.Slug != "pion" || got.Visible || got.ImageVersion != 3 || got.Position != p.Position {
		t.Fatalf("got %+v", got)
	}
}

func TestListFiltersAndOrders(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	create(t, s, "A", true)
	create(t, s, "B", false)
	create(t, s, "C", true)
	all, _ := s.ListPaintings(ctx, false)
	if got := titles(all); len(got) != 3 || got[0] != "C" || got[1] != "B" || got[2] != "A" {
		t.Fatalf("all = %v, want [C B A]", got)
	}
	visible, _ := s.ListPaintings(ctx, true)
	if got := titles(visible); len(got) != 2 || got[0] != "C" || got[1] != "A" {
		t.Fatalf("visible = %v, want [C A]", got)
	}
}

func TestReorder(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a := create(t, s, "A", true)
	b := create(t, s, "B", true)
	c := create(t, s, "C", true)
	if err := s.ReorderPaintings(ctx, []int64{a.ID, c.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListPaintings(ctx, false)
	if got := titles(all); got[0] != "A" || got[1] != "C" || got[2] != "B" {
		t.Fatalf("order = %v, want [A C B]", got)
	}
}

func TestDelete(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := create(t, s, "A", true)
	if err := s.DeletePainting(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPainting(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestReopenKeepsDataAndMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	create(t, s, "A", true)
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	all, _ := s.ListPaintings(context.Background(), false)
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL — `undefined: Open` (and others)

- [ ] **Step 4: Write the migration**

`internal/store/migrations/001_init.sql`:
```sql
CREATE TABLE paintings (
    id            INTEGER PRIMARY KEY,
    slug          TEXT    NOT NULL UNIQUE,
    title         TEXT    NOT NULL,
    technique     TEXT    NOT NULL DEFAULT '',
    size          TEXT    NOT NULL DEFAULT '',
    year          INTEGER,
    description   TEXT    NOT NULL DEFAULT '',
    visible       INTEGER NOT NULL DEFAULT 1,
    position      INTEGER NOT NULL,
    crop_x        INTEGER NOT NULL DEFAULT 0,
    crop_y        INTEGER NOT NULL DEFAULT 0,
    crop_w        INTEGER NOT NULL DEFAULT 0,
    crop_h        INTEGER NOT NULL DEFAULT 0,
    rotation      INTEGER NOT NULL DEFAULT 0,
    image_version INTEGER NOT NULL DEFAULT 1,
    image_width   INTEGER NOT NULL DEFAULT 0,
    image_height  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
);

CREATE INDEX paintings_position ON paintings (position, id);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL
);
```

- [ ] **Step 5: Write `store.go`**

`internal/store/store.go`:
```go
// Package store keeps paintings, site settings and admin sessions in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

const timeFormat = time.RFC3339

// Store is the SQLite-backed data store.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database file and applies migrations.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serialises writes and avoids SQLITE_BUSY; plenty for this site.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("store: read user_version: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries { // ReadDir returns entries sorted by name
		n, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("store: bad migration name %q", e.Name())
		}
		if n <= current {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %s: %w", e.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(timeFormat) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(timeFormat, s)
	return t
}
```

- [ ] **Step 6: Write `paintings.go`**

`internal/store/paintings.go`:
```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/slug"
)

// Painting is one artwork shown on the site.
type Painting struct {
	ID          int64
	Slug        string
	Title       string
	Technique   string
	Size        string
	Year        int // 0 means not specified
	Description string
	Visible     bool
	Position    int // lower comes first on the site

	Crop         images.Crop
	ImageVersion int
	ImageWidth   int
	ImageHeight  int

	CreatedAt time.Time
	UpdatedAt time.Time
}

const paintingCols = `id, slug, title, technique, size, year, description, visible, position,
	crop_x, crop_y, crop_w, crop_h, rotation, image_version, image_width, image_height,
	created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanPainting(row scanner) (Painting, error) {
	var (
		p                  Painting
		year               sql.NullInt64
		created, updated   string
	)
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Technique, &p.Size, &year, &p.Description, &p.Visible, &p.Position,
		&p.Crop.X, &p.Crop.Y, &p.Crop.W, &p.Crop.H, &p.Crop.Rotation, &p.ImageVersion, &p.ImageWidth, &p.ImageHeight,
		&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Painting{}, ErrNotFound
	}
	if err != nil {
		return Painting{}, err
	}
	p.Year = int(year.Int64)
	p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
	return p, nil
}

func nullYear(y int) any {
	if y == 0 {
		return nil
	}
	return y
}

// CreatePainting inserts p at the top of the list and fills in ID, Slug,
// Position and timestamps.
func (s *Store) CreatePainting(ctx context.Context, p *Painting) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	base := slug.Make(p.Title)
	if base == "" {
		base = "kartina"
	}
	sl, err := uniqueSlug(ctx, tx, base)
	if err != nil {
		return err
	}
	var minPos sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MIN(position) FROM paintings").Scan(&minPos); err != nil {
		return err
	}
	pos := 0
	if minPos.Valid {
		pos = int(minPos.Int64) - 1
	}
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO paintings
		(slug, title, technique, size, year, description, visible, position,
		 crop_x, crop_y, crop_w, crop_h, rotation, image_version, image_width, image_height, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sl, p.Title, p.Technique, p.Size, nullYear(p.Year), p.Description, p.Visible, pos,
		p.Crop.X, p.Crop.Y, p.Crop.W, p.Crop.H, p.Crop.Rotation, p.ImageVersion, p.ImageWidth, p.ImageHeight, ts, ts)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.ID, p.Slug, p.Position = id, sl, pos
	p.CreatedAt, p.UpdatedAt = parseTime(ts), parseTime(ts)
	return nil
}

func uniqueSlug(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	for i := 1; ; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", base, i)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM paintings WHERE slug = ?)", cand).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return cand, nil
		}
	}
}

// UpdatePainting saves every field except Slug, Position and CreatedAt.
func (s *Store) UpdatePainting(ctx context.Context, p *Painting) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `UPDATE paintings SET
		title = ?, technique = ?, size = ?, year = ?, description = ?, visible = ?,
		crop_x = ?, crop_y = ?, crop_w = ?, crop_h = ?, rotation = ?,
		image_version = ?, image_width = ?, image_height = ?, updated_at = ?
		WHERE id = ?`,
		p.Title, p.Technique, p.Size, nullYear(p.Year), p.Description, p.Visible,
		p.Crop.X, p.Crop.Y, p.Crop.W, p.Crop.H, p.Crop.Rotation,
		p.ImageVersion, p.ImageWidth, p.ImageHeight, ts, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	p.UpdatedAt = parseTime(ts)
	return nil
}

// GetPainting returns the painting with the given id.
func (s *Store) GetPainting(ctx context.Context, id int64) (Painting, error) {
	return scanPainting(s.db.QueryRowContext(ctx, "SELECT "+paintingCols+" FROM paintings WHERE id = ?", id))
}

// GetPaintingBySlug returns the painting with the given slug.
func (s *Store) GetPaintingBySlug(ctx context.Context, sl string) (Painting, error) {
	return scanPainting(s.db.QueryRowContext(ctx, "SELECT "+paintingCols+" FROM paintings WHERE slug = ?", sl))
}

// ListPaintings returns paintings in site order.
func (s *Store) ListPaintings(ctx context.Context, visibleOnly bool) ([]Painting, error) {
	q := "SELECT " + paintingCols + " FROM paintings"
	if visibleOnly {
		q += " WHERE visible = 1"
	}
	q += " ORDER BY position, id"
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Painting
	for rows.Next() {
		p, err := scanPainting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReorderPaintings sets each painting's position to its index in ids.
func (s *Store) ReorderPaintings(ctx context.Context, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE paintings SET position = ? WHERE id = ?", i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeletePainting removes the painting row.
func (s *Store) DeletePainting(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM paintings WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go mod tidy && gofmt -w internal/store && go test ./internal/store/`
Expected: `ok`

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod go.sum internal/store
git commit -m "feat: add SQLite store with paintings"
```

---

### Task 5: Store — settings, sessions, backup

**Files:**
- Create: `internal/store/settings.go`
- Create: `internal/store/sessions.go`
- Modify: `internal/store/store.go` (add `Backup`)
- Test: `internal/store/settings_test.go`

**Interfaces:**
- Produces:
  - `func (s *Store) Setting(ctx, key string) (string, error)` — `""` when unset
  - `func (s *Store) Settings(ctx) (map[string]string, error)`
  - `func (s *Store) SetSettings(ctx, values map[string]string) error` — upsert all in one transaction
  - `func (s *Store) CreateSession(ctx, tokenHash string, expires time.Time) error`
  - `func (s *Store) SessionValid(ctx, tokenHash string, now time.Time) (bool, error)`
  - `func (s *Store) DeleteSession(ctx, tokenHash string) error`
  - `func (s *Store) DeleteExpiredSessions(ctx, now time.Time) error`
  - `func (s *Store) Backup(ctx, dest string) error` — consistent snapshot via `VACUUM INTO`

- [ ] **Step 1: Write the failing tests**

`internal/store/settings_test.go`:
```go
package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if v, err := s.Setting(ctx, "artist_name"); err != nil || v != "" {
		t.Fatalf("unset setting = %q, %v", v, err)
	}
	if err := s.SetSettings(ctx, map[string]string{"artist_name": "Анна", "phone": "+7 900"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(ctx, map[string]string{"phone": "+7 901"}); err != nil {
		t.Fatal(err)
	}
	all, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all["artist_name"] != "Анна" || all["phone"] != "+7 901" || len(all) != 2 {
		t.Fatalf("settings = %v", all)
	}
	if v, _ := s.Setting(ctx, "phone"); v != "+7 901" {
		t.Fatalf("phone = %q", v)
	}
}

func TestSessions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := s.CreateSession(ctx, "h1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "h2", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SessionValid(ctx, "h1", now); !ok {
		t.Error("h1 should be valid")
	}
	if ok, _ := s.SessionValid(ctx, "h2", now); ok {
		t.Error("h2 is expired")
	}
	if ok, _ := s.SessionValid(ctx, "missing", now); ok {
		t.Error("missing session must be invalid")
	}
	if err := s.DeleteExpiredSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 1 {
		t.Fatalf("sessions left = %d, want 1", n)
	}
	if err := s.DeleteSession(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SessionValid(ctx, "h1", now); ok {
		t.Error("h1 was deleted")
	}
}

func TestBackup(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	create(t, s, "Пион", true)
	dest := filepath.Join(t.TempDir(), "backup", "gallery.db")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	// A second backup must overwrite the first.
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	all, _ := b.ListPaintings(ctx, false)
	if len(all) != 1 || all[0].Title != "Пион" {
		t.Fatalf("backup contents = %v", titles(all))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL — `s.Setting undefined` (and others)

- [ ] **Step 3: Write the implementation**

`internal/store/settings.go`:
```go
package store

import (
	"context"
	"database/sql"
	"errors"
)

// Setting returns one setting value, or "" when it is not set.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Settings returns all settings.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSettings inserts or replaces the given settings in one transaction.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

`internal/store/sessions.go`:
```go
package store

import (
	"context"
	"time"
)

// CreateSession stores the hash of a new session token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions (token_hash, expires_at) VALUES (?, ?)",
		tokenHash, expires.UTC().Format(timeFormat))
	return err
}

// SessionValid reports whether the session exists and has not expired.
func (s *Store) SessionValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash = ? AND expires_at > ?)",
		tokenHash, now.UTC().Format(timeFormat)).Scan(&ok)
	return ok, err
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteExpiredSessions removes sessions that expired before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.UTC().Format(timeFormat))
	return err
}
```

Append to `internal/store/store.go` (add `"os"` and `"path/filepath"` to its imports):
```go
// Backup writes a consistent snapshot of the database to dest, replacing it.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dest)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/store
git commit -m "feat: add settings, sessions and backup to store"
```

---

### Task 6: Site settings view (`site` package)

**Files:**
- Create: `internal/site/site.go`
- Test: `internal/site/site_test.go`

**Interfaces:**
- Consumes: `store.Store.Settings` (Task 5), `images.Crop` (Task 2).
- Produces:
  - Setting key constants: `KeyArtistName="artist_name"`, `KeySubtitle="subtitle"`, `KeyGreeting="greeting"`, `KeyAboutText="about_text"`, `KeyAboutPhoto="about_photo"`, `KeyPhone="phone"`, `KeyEmail="email"`, `KeyTelegram="telegram"`, `KeyWhatsApp="whatsapp"`, `KeyVK="vk"`
  - `const DefaultArtistName = "Художник"`
  - `type Photo struct { Version, Width, Height int; Crop images.Crop }` (JSON tags `version,width,height,crop`; stored as JSON under `KeyAboutPhoto`)
  - `type Info struct { ArtistName, Subtitle, Greeting, AboutText string; AboutPhoto *Photo; Phone, Email, Telegram, WhatsApp, VK string }`
  - `type Link struct { Label, Text string; Href template.URL }`
  - `func Load(ctx context.Context, st *store.Store) (Info, error)`
  - `func (i Info) Contacts() []Link` — only filled fields, order: phone, email, Telegram, WhatsApp, VK

`Href` is `template.URL` because `html/template` would otherwise replace `tel:` links with `#ZgotmplZ`. Every `Href` is built here from a fixed safe scheme (`tel:`, `mailto:`, `https://`, or an `http(s)://` link the admin typed).

- [ ] **Step 1: Write the failing tests**

`internal/site/site_test.go`:
```go
package site

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestLoadDefaults(t *testing.T) {
	info, err := Load(context.Background(), openStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.ArtistName != DefaultArtistName || info.AboutPhoto != nil || len(info.Contacts()) != 0 {
		t.Fatalf("defaults = %+v", info)
	}
}

func TestLoadReadsSettings(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	photo, _ := json.Marshal(Photo{Version: 2, Width: 300, Height: 400, Crop: images.Crop{W: 10, H: 10}})
	st.SetSettings(ctx, map[string]string{
		KeyArtistName: "Анна Иванова", KeySubtitle: "художник, акварель", KeyGreeting: "Здравствуйте!",
		KeyAboutText: "Обо мне", KeyAboutPhoto: string(photo), KeyPhone: "+7 900 000-00-00",
	})
	info, err := Load(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if info.ArtistName != "Анна Иванова" || info.Subtitle != "художник, акварель" || info.Greeting != "Здравствуйте!" ||
		info.AboutText != "Обо мне" || info.Phone != "+7 900 000-00-00" {
		t.Fatalf("info = %+v", info)
	}
	if info.AboutPhoto == nil || info.AboutPhoto.Version != 2 || info.AboutPhoto.Width != 300 {
		t.Fatalf("AboutPhoto = %+v", info.AboutPhoto)
	}
}

func TestLoadIgnoresBrokenPhotoJSON(t *testing.T) {
	st := openStore(t)
	st.SetSettings(context.Background(), map[string]string{KeyAboutPhoto: "{broken"})
	info, err := Load(context.Background(), st)
	if err != nil || info.AboutPhoto != nil {
		t.Fatalf("info.AboutPhoto = %+v, err = %v", info.AboutPhoto, err)
	}
}

func TestContacts(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want Link
	}{
		{"phone", Info{Phone: "+7 (900) 000-00-00"}, Link{"Телефон", "+7 (900) 000-00-00", "tel:+79000000000"}},
		{"email", Info{Email: "anna@example.ru"}, Link{"Почта", "anna@example.ru", "mailto:anna@example.ru"}},
		{"telegram username", Info{Telegram: "@anna_art"}, Link{"Telegram", "@anna_art", "https://t.me/anna_art"}},
		{"telegram bare", Info{Telegram: "anna_art"}, Link{"Telegram", "@anna_art", "https://t.me/anna_art"}},
		{"telegram link", Info{Telegram: "https://t.me/anna_art"}, Link{"Telegram", "t.me/anna_art", "https://t.me/anna_art"}},
		{"telegram link without scheme", Info{Telegram: "t.me/anna_art"}, Link{"Telegram", "t.me/anna_art", "https://t.me/anna_art"}},
		{"whatsapp 8", Info{WhatsApp: "8 900 000-00-00"}, Link{"WhatsApp", "8 900 000-00-00", "https://wa.me/79000000000"}},
		{"whatsapp +7", Info{WhatsApp: "+7 900 000 00 00"}, Link{"WhatsApp", "+7 900 000 00 00", "https://wa.me/79000000000"}},
		{"vk id", Info{VK: "anna.art"}, Link{"ВКонтакте", "vk.com/anna.art", "https://vk.com/anna.art"}},
		{"vk link", Info{VK: "https://vk.com/anna.art"}, Link{"ВКонтакте", "vk.com/anna.art", "https://vk.com/anna.art"}},
		{"vk link without scheme", Info{VK: "vk.com/anna.art"}, Link{"ВКонтакте", "vk.com/anna.art", "https://vk.com/anna.art"}},
	}
	for _, tt := range tests {
		got := tt.info.Contacts()
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestContactsOrderAndSkipsEmpty(t *testing.T) {
	info := Info{VK: "x", Phone: "1", Email: "a@b.c", Telegram: "  "}
	got := info.Contacts()
	if len(got) != 3 || got[0].Label != "Телефон" || got[1].Label != "Почта" || got[2].Label != "ВКонтакте" {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/site/`
Expected: FAIL — `undefined: Load` (and others)

- [ ] **Step 3: Write the implementation**

`internal/site/site.go`:
```go
// Package site gives a typed view of the site settings the painter edits:
// her name, texts, contacts and the author photo.
package site

import (
	"context"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

// Setting keys.
const (
	KeyArtistName = "artist_name"
	KeySubtitle   = "subtitle"
	KeyGreeting   = "greeting"
	KeyAboutText  = "about_text"
	KeyAboutPhoto = "about_photo"
	KeyPhone      = "phone"
	KeyEmail      = "email"
	KeyTelegram   = "telegram"
	KeyWhatsApp   = "whatsapp"
	KeyVK         = "vk"
)

// DefaultArtistName is shown until the painter enters her name.
const DefaultArtistName = "Художник"

// Photo describes the processed author photo.
type Photo struct {
	Version int         `json:"version"`
	Width   int         `json:"width"`
	Height  int         `json:"height"`
	Crop    images.Crop `json:"crop"`
}

// Info is everything the page layout needs to know about the site.
type Info struct {
	ArtistName string
	Subtitle   string
	Greeting   string
	AboutText  string
	AboutPhoto *Photo // nil when no photo was uploaded

	Phone    string
	Email    string
	Telegram string
	WhatsApp string
	VK       string
}

// Link is one clickable contact.
type Link struct {
	Label string
	Text  string
	Href  template.URL
}

// Load reads the settings from the store.
func Load(ctx context.Context, st *store.Store) (Info, error) {
	m, err := st.Settings(ctx)
	if err != nil {
		return Info{}, err
	}
	info := Info{
		ArtistName: m[KeyArtistName],
		Subtitle:   m[KeySubtitle],
		Greeting:   m[KeyGreeting],
		AboutText:  m[KeyAboutText],
		Phone:      m[KeyPhone],
		Email:      m[KeyEmail],
		Telegram:   m[KeyTelegram],
		WhatsApp:   m[KeyWhatsApp],
		VK:         m[KeyVK],
	}
	if strings.TrimSpace(info.ArtistName) == "" {
		info.ArtistName = DefaultArtistName
	}
	if raw := m[KeyAboutPhoto]; raw != "" {
		var p Photo
		if err := json.Unmarshal([]byte(raw), &p); err == nil && p.Version > 0 {
			info.AboutPhoto = &p
		}
	}
	return info, nil
}

// Contacts returns links for the filled-in contact fields.
func (i Info) Contacts() []Link {
	var out []Link
	if v := strings.TrimSpace(i.Phone); v != "" {
		out = append(out, Link{"Телефон", v, template.URL("tel:" + keep(v, "+0123456789"))})
	}
	if v := strings.TrimSpace(i.Email); v != "" {
		out = append(out, Link{"Почта", v, template.URL("mailto:" + v)})
	}
	if v := strings.TrimSpace(i.Telegram); v != "" {
		if u := asLink(v); u != "" {
			out = append(out, Link{"Telegram", display(u), template.URL(u)})
		} else {
			name := strings.TrimPrefix(v, "@")
			out = append(out, Link{"Telegram", "@" + name, template.URL("https://t.me/" + url.PathEscape(name))})
		}
	}
	if v := strings.TrimSpace(i.WhatsApp); v != "" {
		digits := keep(v, "0123456789")
		if len(digits) == 11 && digits[0] == '8' {
			digits = "7" + digits[1:]
		}
		if digits != "" {
			out = append(out, Link{"WhatsApp", v, template.URL("https://wa.me/" + digits)})
		}
	}
	if v := strings.TrimSpace(i.VK); v != "" {
		u := asLink(v)
		if u == "" {
			u = "https://vk.com/" + url.PathEscape(v)
		}
		out = append(out, Link{"ВКонтакте", display(u), template.URL(u)})
	}
	return out
}

// keep returns s with only the runes listed in allowed.
func keep(s, allowed string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(allowed, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// asLink returns v as an https/http URL if it looks like a link, else "".
func asLink(v string) string {
	lower := strings.ToLower(v)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		return v
	case strings.Contains(v, "/"):
		return "https://" + v
	}
	return ""
}

// display strips the scheme, "www." and a trailing slash for showing a link.
func display(u string) string {
	for _, p := range []string{"https://", "http://", "www."} {
		if strings.HasPrefix(strings.ToLower(u), p) {
			u = u[len(p):]
		}
	}
	return strings.TrimSuffix(u, "/")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/site/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/site
git commit -m "feat: add typed site settings and contact links"
```

---

### Task 7: Gallery use cases

**Files:**
- Create: `internal/gallery/gallery.go`
- Test: `internal/gallery/gallery_test.go`

**Interfaces:**
- Consumes: `store` (Tasks 4–5), `images.Process/DetectExt/Disk/PaintingDir/AboutDir/NormalizeRotation` (Tasks 2–3), `site.Load/Photo/KeyAboutPhoto` (Task 6).
- Produces:
  - `type Gallery struct { Store *store.Store; Disk *images.Disk }`
  - `type PaintingInput struct { Title, Technique, Size string; Year int; Description string; Visible bool }`
  - `type FieldErrors map[string]string` — keys are form field names (`"title"`, `"year"`, `"photo"`, …), values are Russian messages
  - `func (in PaintingInput) Validate(now time.Time) FieldErrors` — never nil
  - `type Photo struct { Original []byte; Crop images.Crop }` — `Original == nil` means "re-crop the stored original"
  - `var ErrNoPhoto error`
  - `func CleanText(s string) string` — CRLF→LF, trim
  - `func (g *Gallery) AddPainting(ctx, in PaintingInput, photo Photo) (store.Painting, error)`
  - `func (g *Gallery) UpdatePainting(ctx, id int64, in PaintingInput, photo *Photo) (store.Painting, error)` — `photo == nil` keeps the current image
  - `func (g *Gallery) DeletePainting(ctx, id int64) error`
  - `func (g *Gallery) SetAboutPhoto(ctx, photo Photo) error`
  - Errors: `images.ErrDecode` for unreadable photos, `store.ErrNotFound` for unknown ids, `ErrNoPhoto`.

- [ ] **Step 1: Write the failing tests**

`internal/gallery/gallery_test.go`:
```go
package gallery

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func newGallery(t *testing.T) *Gallery {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Gallery{Store: st, Disk: &images.Disk{Root: filepath.Join(dir, "images")}}
}

func exists(t *testing.T, g *Gallery, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(g.Disk.Root, filepath.FromSlash(dir), name))
	return err == nil
}

func photo(t *testing.T) Photo {
	return Photo{Original: testutil.JPEG(t, 300, 200, color.White)}
}

func TestValidate(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if errs := (PaintingInput{Title: "Пион", Year: 2025}).Validate(now); len(errs) != 0 {
		t.Fatalf("valid input got errors %v", errs)
	}
	errs := (PaintingInput{Title: "   ", Year: 1800}).Validate(now)
	if errs["title"] != "Укажите название" {
		t.Errorf("title error = %q", errs["title"])
	}
	if errs["year"] != "Год должен быть от 1900 до 2026" {
		t.Errorf("year error = %q", errs["year"])
	}
	if errs := (PaintingInput{Title: "x", Year: 2027}).Validate(now); errs["year"] == "" {
		t.Error("future year must be rejected")
	}
}

func TestAddPaintingStoresRowAndFiles(t *testing.T) {
	g := newGallery(t)
	p, err := g.AddPainting(context.Background(), PaintingInput{
		Title: "  Пион ", Technique: "Бумага, акварель", Description: "Строка\r\nвторая\r\n", Visible: true,
	}, Photo{Original: testutil.JPEG(t, 300, 200, color.White), Crop: images.Crop{W: 100, H: 100, Rotation: -90}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Пион" || p.Description != "Строка\nвторая" || p.Slug != "pion" {
		t.Fatalf("painting = %+v", p)
	}
	if p.ImageVersion != 1 || p.Crop.Rotation != 270 {
		t.Fatalf("version = %d, rotation = %d", p.ImageVersion, p.Crop.Rotation)
	}
	dir := images.PaintingDir(p.ID)
	for _, name := range []string{"original.jpg", "v1-600.jpg", "v1-1200.jpg", "v1-2000.jpg"} {
		if !exists(t, g, dir, name) {
			t.Errorf("%s/%s missing", dir, name)
		}
	}
}

func TestAddPaintingRequiresPhoto(t *testing.T) {
	g := newGallery(t)
	if _, err := g.AddPainting(context.Background(), PaintingInput{Title: "x"}, Photo{}); !errors.Is(err, ErrNoPhoto) {
		t.Fatalf("err = %v, want ErrNoPhoto", err)
	}
}

func TestAddPaintingWithBadPhotoLeavesNothing(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	_, err := g.AddPainting(ctx, PaintingInput{Title: "x"}, Photo{Original: []byte("nope")})
	if !errors.Is(err, images.ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
	if all, _ := g.Store.ListPaintings(ctx, false); len(all) != 0 {
		t.Fatalf("no painting should be stored, got %d", len(all))
	}
}

func TestUpdateTextOnlyKeepsImage(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион", Visible: true}, photo(t))
	got, err := g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Розовый пион", Year: 2025}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Розовый пион" || got.Year != 2025 || got.Visible || got.ImageVersion != 1 || got.Slug != "pion" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateRecropReusesOriginalAndBumpsVersion(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион"}, photo(t))
	got, err := g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Пион"}, &Photo{Crop: images.Crop{X: 0, Y: 0, W: 100, H: 50}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageVersion != 2 || got.ImageWidth != 100 || got.ImageHeight != 50 {
		t.Fatalf("got %+v", got)
	}
	dir := images.PaintingDir(p.ID)
	if exists(t, g, dir, "v1-600.jpg") || !exists(t, g, dir, "v2-600.jpg") || !exists(t, g, dir, "original.jpg") {
		t.Fatal("expected v2 variants, no v1 variants, original kept")
	}
}

func TestUpdateReplacesOriginal(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион"}, photo(t))
	got, err := g.UpdatePainting(ctx, p.ID, PaintingInput{Title: "Пион"}, &Photo{Original: testutil.PNG(t, 40, 30, color.Black)})
	if err != nil {
		t.Fatal(err)
	}
	dir := images.PaintingDir(p.ID)
	if got.ImageWidth != 40 || !exists(t, g, dir, "original.png") || exists(t, g, dir, "original.jpg") {
		t.Fatalf("original not replaced: %+v", got)
	}
}

func TestUpdateUnknownPainting(t *testing.T) {
	g := newGallery(t)
	if _, err := g.UpdatePainting(context.Background(), 99, PaintingInput{Title: "x"}, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletePaintingRemovesFiles(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	p, _ := g.AddPainting(ctx, PaintingInput{Title: "Пион"}, photo(t))
	if err := g.DeletePainting(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(g.Disk.Root, "paintings", "1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("painting dir should be removed")
	}
	if _, err := g.Store.GetPainting(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("row should be removed")
	}
}

func TestSetAboutPhoto(t *testing.T) {
	g := newGallery(t)
	ctx := context.Background()
	if err := g.SetAboutPhoto(ctx, photo(t)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetAboutPhoto(ctx, Photo{Crop: images.Crop{W: 50, H: 50}}); err != nil { // re-crop
		t.Fatal(err)
	}
	info, _ := site.Load(ctx, g.Store)
	if info.AboutPhoto == nil || info.AboutPhoto.Version != 2 || info.AboutPhoto.Width != 50 {
		t.Fatalf("AboutPhoto = %+v", info.AboutPhoto)
	}
	if exists(t, g, images.AboutDir, "v1-600.jpg") || !exists(t, g, images.AboutDir, "v2-600.jpg") {
		t.Fatal("expected only v2 variants")
	}
}

func TestSetAboutPhotoRecropWithoutOriginalFails(t *testing.T) {
	g := newGallery(t)
	if err := g.SetAboutPhoto(context.Background(), Photo{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gallery/`
Expected: FAIL — `undefined: Gallery` (and others)

- [ ] **Step 3: Write the implementation**

`internal/gallery/gallery.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gallery/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/gallery
git commit -m "feat: add gallery use cases for paintings and author photo"
```

---

### Task 8: Public site skeleton — assets, layout, home page, media

**Files:**
- Create: `internal/assets/assets.go`
- Create: `internal/assets/static/site.css`
- Create: `internal/assets/static/fonts/*.woff2`, `internal/assets/static/fonts/LICENSE-*.txt` (downloaded)
- Create: `internal/web/server.go`
- Create: `internal/web/funcs.go`
- Create: `internal/web/home.go`
- Create: `internal/web/templates/layout.html`, `home.html`, `notfound.html`, `error.html`
- Test: `internal/web/funcs_test.go`, `internal/web/helpers_test.go`, `internal/web/home_test.go`

**Interfaces:**
- Consumes: `store`, `images.URL/SrcSet/PaintingDir/AboutDir/Disk.VariantFile`, `site.Load/Info`, `gallery` (tests only).
- Produces:
  - `assets.Handler() http.Handler` — serves embedded `static/` under `/static/`
  - `type web.Config struct { Store *store.Store; Disk *images.Disk; BaseURL string; Log *slog.Logger }`
  - `func web.New(cfg Config) (*Server, error)`, `func (s *Server) Register(mux *http.ServeMux)` — registers public routes **and** `GET /static/` and the catch-all `/` 404
  - Template funcs available to all public templates: `paragraphs`, `lines`, `details`, `imgURL`, `srcset`, `paintingDir`
  - Templates: every page file defines `content` and optionally `scripts`; executed through `layout`
  - `page` struct fields used by layout: `Site, Title, Description, CanonicalURL, OGImage, OGType, Nav, Data`

- [ ] **Step 1: Download fonts**

```bash
mkdir -p internal/assets/static/fonts
cd internal/assets/static/fonts
base=https://cdn.jsdelivr.net/npm/@fontsource
for f in lora-cyrillic-400-normal lora-latin-400-normal lora-cyrillic-500-normal lora-latin-500-normal lora-cyrillic-400-italic lora-latin-400-italic; do
  curl -fsSLo "$f.woff2" "$base/lora@5/files/$f.woff2"
done
for f in marck-script-cyrillic-400-normal marck-script-latin-400-normal; do
  curl -fsSLo "$f.woff2" "$base/marck-script@5/files/$f.woff2"
done
curl -fsSLo LICENSE-lora.txt "$base/lora@5/LICENSE"
curl -fsSLo LICENSE-marck-script.txt "$base/marck-script@5/LICENSE"
file *.woff2
cd -
```
Expected: every `.woff2` reported as `Web Open Font Format (Version 2)`.

- [ ] **Step 2: Write `assets.go`**

`internal/assets/assets.go`:
```go
// Package assets embeds the static files (CSS, JS, fonts, vendored libraries).
package assets

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var files embed.FS

// Handler serves the embedded files under /static/.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") { // no directory listings
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 3: Write `site.css`**

`internal/assets/static/site.css`:
```css
@font-face { font-family: "Lora"; font-style: normal; font-weight: 400; font-display: swap; src: url(fonts/lora-cyrillic-400-normal.woff2) format("woff2"); unicode-range: U+0301, U+0400-045F, U+0490-0491, U+04B0-04B1, U+2116; }
@font-face { font-family: "Lora"; font-style: normal; font-weight: 400; font-display: swap; src: url(fonts/lora-latin-400-normal.woff2) format("woff2"); unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD; }
@font-face { font-family: "Lora"; font-style: normal; font-weight: 500; font-display: swap; src: url(fonts/lora-cyrillic-500-normal.woff2) format("woff2"); unicode-range: U+0301, U+0400-045F, U+0490-0491, U+04B0-04B1, U+2116; }
@font-face { font-family: "Lora"; font-style: normal; font-weight: 500; font-display: swap; src: url(fonts/lora-latin-500-normal.woff2) format("woff2"); unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD; }
@font-face { font-family: "Lora"; font-style: italic; font-weight: 400; font-display: swap; src: url(fonts/lora-cyrillic-400-italic.woff2) format("woff2"); unicode-range: U+0301, U+0400-045F, U+0490-0491, U+04B0-04B1, U+2116; }
@font-face { font-family: "Lora"; font-style: italic; font-weight: 400; font-display: swap; src: url(fonts/lora-latin-400-italic.woff2) format("woff2"); unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD; }
@font-face { font-family: "Marck Script"; font-style: normal; font-weight: 400; font-display: swap; src: url(fonts/marck-script-cyrillic-400-normal.woff2) format("woff2"); unicode-range: U+0301, U+0400-045F, U+0490-0491, U+04B0-04B1, U+2116; }
@font-face { font-family: "Marck Script"; font-style: normal; font-weight: 400; font-display: swap; src: url(fonts/marck-script-latin-400-normal.woff2) format("woff2"); unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD; }

:root {
  --paper: #efe8dc;
  --paper-light: #f8f4ec;
  --mat: #fdfbf7;
  --ink: #3b332b;
  --ink-soft: #4a3f35;
  --muted: #8b7a66;
  --accent: #9c4a2f;
  --line: #cdbfa9;
  --shadow: 0 1px 2px rgba(60, 40, 20, .12), 0 6px 18px rgba(60, 40, 20, .10);
}

* { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body { margin: 0; background: var(--paper); color: var(--ink); font: 17px/1.6 "Lora", Georgia, serif; }
a { color: var(--accent); }
img { max-width: 100%; height: auto; display: block; }

.wrap { max-width: 1200px; margin: 0 auto; padding: 24px 20px 56px; }

/* header */
.hdr { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: flex-end; gap: 12px 32px; margin-bottom: 28px; }
.brand { display: block; color: inherit; text-decoration: none; }
.name { display: block; font-family: "Marck Script", cursive; font-size: 44px; line-height: 1.05; color: #4a3b2e; }
.sub { display: block; margin-top: 4px; font-style: italic; color: var(--muted); }
.nav { display: flex; gap: 20px; font-size: 16px; }
.nav a { padding: 6px 0; color: #6b5c4d; text-decoration: none; }
.nav a.cur, .nav a:hover { color: var(--accent); }

/* home */
.intro { display: flex; flex-direction: column; gap: 16px; margin-bottom: 36px; padding: 20px 22px; border-radius: 4px; background: var(--paper-light); color: #5a4c3f; }
.intro p { margin: 0 0 .5em; }
.intro p:last-child { margin-bottom: 0; }
.intro-photo { flex: none; width: 72px; height: 72px; border-radius: 50%; object-fit: cover; }
.grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: 1fr; gap: 36px; align-items: start; }
.card a { display: block; color: inherit; text-decoration: none; }
.mat { background: var(--mat); padding: 12px; box-shadow: var(--shadow); }
.card .mat { transition: transform .2s, box-shadow .2s; }
.card a:hover .mat { transform: translateY(-2px); box-shadow: 0 2px 4px rgba(60, 40, 20, .14), 0 10px 26px rgba(60, 40, 20, .14); }
.t { margin: 14px 0 0; font-size: 20px; font-weight: 500; line-height: 1.3; }
.m { margin: 4px 0 0; font-size: 15px; font-style: italic; color: var(--muted); }
.empty { color: var(--muted); font-style: italic; }

/* painting page */
.back { display: inline-block; margin-bottom: 20px; font-size: 15px; color: var(--muted); text-decoration: none; }
.work { display: grid; gap: 28px; }
.work .image { text-align: center; }
.work .zoom { display: inline-block; max-width: 100%; cursor: zoom-in; }
.work h1 { margin: 0; font-size: 28px; font-weight: 500; line-height: 1.2; }
.work .meta { margin: 8px 0 0; font-style: italic; color: var(--muted); }
.story { margin-top: 20px; color: var(--ink-soft); line-height: 1.75; }
.story p { margin: 0 0 1em; }
.hint { margin: 10px 0 0; font-size: 13px; font-style: italic; color: #a8987f; }
.pn { display: flex; justify-content: space-between; gap: 16px; margin-top: 36px; padding-top: 18px; border-top: 1px dashed var(--line); font-size: 15px; }
.pn a { text-decoration: none; }
.lightbox { position: fixed; inset: 0; z-index: 100; display: flex; align-items: center; justify-content: center; padding: 16px; background: rgba(20, 16, 12, .94); cursor: zoom-out; }
.lightbox[hidden] { display: none; }
.lightbox img { width: auto; height: auto; max-width: 100%; max-height: 100%; object-fit: contain; }
.lightbox-close { position: absolute; top: 8px; right: 12px; border: 0; background: none; color: #fff; font-size: 40px; line-height: 1; cursor: pointer; }

/* about, contacts, errors */
.page h1 { margin: 0 0 20px; font-size: 28px; font-weight: 500; }
.about { display: grid; gap: 28px; align-items: start; }
.contacts { list-style: none; margin: 0; padding: 0; display: grid; gap: 14px; }
.contacts .label { display: block; font-size: 14px; font-style: italic; color: var(--muted); }
.contacts a { font-size: 19px; text-decoration: none; }

/* footer */
.foot { display: flex; flex-wrap: wrap; gap: 8px 24px; margin-top: 64px; padding-top: 20px; border-top: 1px dashed var(--line); font-size: 15px; }
.foot a { color: var(--muted); text-decoration: none; }
.foot a:hover { color: var(--accent); }

@media (min-width: 640px) {
  .wrap { padding: 36px 40px 72px; }
  .name { font-size: 52px; }
  .nav { gap: 28px; }
  .grid { grid-template-columns: repeat(2, 1fr); gap: 40px; }
  .intro { flex-direction: row; align-items: center; padding: 24px 28px; }
  .mat { padding: 18px; }
}

@media (min-width: 900px) {
  .work { grid-template-columns: 1.15fr 1fr; gap: 56px; align-items: start; }
  .work .image { position: sticky; top: 24px; }
  .work .zoom img { width: auto; max-height: calc(100vh - 140px); }
  .work h1 { font-size: 34px; }
  .about { grid-template-columns: 1fr 1.4fr; gap: 56px; }
  .about.text-only { grid-template-columns: minmax(0, 720px); }
}

@media (min-width: 1000px) {
  .wrap { padding: 40px 56px 80px; }
  .grid { grid-template-columns: repeat(3, 1fr); }
}
```

- [ ] **Step 4: Write the failing tests for template helpers**

`internal/web/funcs_test.go`:
```go
package web

import (
	"reflect"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

func TestParagraphs(t *testing.T) {
	got := paragraphs("Первый абзац\nвторая строка\r\n\r\n\n  Второй  \n \n")
	want := []string{"Первый абзац\nвторая строка", "Второй"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(paragraphs("  \n ")) != 0 {
		t.Fatal("blank text has no paragraphs")
	}
}

func TestDetails(t *testing.T) {
	p := store.Painting{Technique: "Бумага, акварель", Size: "30×40 см", Year: 2025}
	if got := details(p); got != "Бумага, акварель · 30×40 см · 2025" {
		t.Errorf("got %q", got)
	}
	if got := details(store.Painting{Size: "30×40 см"}); got != "30×40 см" {
		t.Errorf("got %q", got)
	}
	if got := details(store.Painting{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("короткий  текст\n", 50); got != "короткий текст" {
		t.Errorf("got %q", got)
	}
	if got := truncate("один два три", 7); got != "один д…" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 5: Write the test environment and failing home/media tests**

`internal/web/helpers_test.go`:
```go
package web

import (
	"context"
	"image/color"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

type env struct {
	t  *testing.T
	st *store.Store
	g  *gallery.Gallery
	h  http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	disk := &images.Disk{Root: filepath.Join(dir, "images")}
	srv, err := New(Config{Store: st, Disk: disk, BaseURL: "https://example.ru", Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	return &env{t: t, st: st, g: &gallery.Gallery{Store: st, Disk: disk}, h: mux}
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func (e *env) add(in gallery.PaintingInput) store.Painting {
	e.t.Helper()
	p, err := e.g.AddPainting(context.Background(), in, gallery.Photo{Original: testutil.JPEG(e.t, 300, 200, color.White)})
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) settings(kv map[string]string) {
	e.t.Helper()
	if err := e.st.SetSettings(context.Background(), kv); err != nil {
		e.t.Fatal(err)
	}
}
```

`internal/web/home_test.go`:
```go
package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

func TestHomeListsVisiblePaintingsInOrder(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Карпы кои", Technique: "Бумага, акварель", Visible: true})
	e.add(gallery.PaintingInput{Title: "Скрытая работа", Visible: false})
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})

	rec := e.get("/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(body, "Скрытая работа") {
		t.Error("hidden painting must not be listed")
	}
	iPeony, iKoi := strings.Index(body, "Пион"), strings.Index(body, "Карпы кои")
	if iPeony < 0 || iKoi < 0 || iPeony > iKoi {
		t.Errorf("want Пион (newest) before Карпы кои; got indexes %d, %d", iPeony, iKoi)
	}
	for _, want := range []string{`href="/paintings/pion"`, "Бумага, акварель", `/media/paintings/1/v1-600.jpg`, `loading="lazy"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestHomeEmptyState(t *testing.T) {
	body := newEnv(t).get("/").Body.String()
	if !strings.Contains(body, "Скоро здесь появятся работы") {
		t.Error("empty state text missing")
	}
	if !strings.Contains(body, site.DefaultArtistName) {
		t.Error("default artist name missing")
	}
}

func TestHomeHeaderGreetingAndFooter(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{
		site.KeyArtistName: "Анна Иванова", site.KeySubtitle: "художник, акварель",
		site.KeyGreeting: "Здравствуйте! Я пишу акварелью.", site.KeyPhone: "+7 900 000-00-00",
	})
	body := e.get("/").Body.String()
	for _, want := range []string{"Анна Иванова", "художник, акварель", "Здравствуйте! Я пишу акварелью.",
		// html/template writes "+" in attributes as "&#43;", which browsers decode back.
		"Подробнее обо мне", `href="tel:&#43;79000000000"`, `<html lang="ru">`, `<title>Анна Иванова — художник, акварель</title>`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestMediaServesVariants(t *testing.T) {
	e := newEnv(t)
	p := e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	rec := e.get(images.URL(images.PaintingDir(p.ID), 1, 600))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestMediaRejectsOriginalsAndMissingFiles(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	for _, path := range []string{
		"/media/paintings/1/original.jpg",
		"/media/paintings/1/v9-600.jpg",
		"/media/paintings/abc/v1-600.jpg",
		"/media/about/v1-600.jpg",
	} {
		rec := e.get(path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
		if strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s: 404 must not be cached forever", path)
		}
	}
}

func TestUnknownPathShowsRussian404(t *testing.T) {
	rec := newEnv(t).get("/no-such-page")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Страница не найдена") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestStaticFiles(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/static/site.css")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("site.css: status = %d, type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := e.get("/static/"); rec.Code != http.StatusNotFound {
		t.Errorf("directory listing: status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/web/`
Expected: FAIL — `undefined: New` (and others)

- [ ] **Step 7: Write `funcs.go`**

`internal/web/funcs.go`:
```go
package web

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

var funcs = template.FuncMap{
	"paragraphs":  paragraphs,
	"lines":       lines,
	"details":     details,
	"imgURL":      images.URL,
	"srcset":      images.SrcSet,
	"paintingDir": images.PaintingDir,
}

var blankLine = regexp.MustCompile(`\n[ \t]*\n`)

// paragraphs splits plain text into paragraphs at blank lines.
func paragraphs(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, p := range blankLine.Split(s, -1) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lines splits a paragraph into its lines (rendered with <br> between them).
func lines(s string) []string { return strings.Split(s, "\n") }

// details is the one-line caption "Техника · Размер · Год".
func details(p store.Painting) string {
	var parts []string
	for _, s := range []string{p.Technique, p.Size} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if p.Year != 0 {
		parts = append(parts, strconv.Itoa(p.Year))
	}
	return strings.Join(parts, " · ")
}

// truncate collapses whitespace and cuts s to n runes, adding an ellipsis.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
```

- [ ] **Step 8: Write `server.go`**

`internal/web/server.go`:
```go
// Package web serves the public site.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"

	"github.com/lilxtent/pictures-gallery/internal/assets"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

// Config holds the server's dependencies.
type Config struct {
	Store   *store.Store
	Disk    *images.Disk
	BaseURL string // absolute site URL without trailing slash, for OG tags and sitemap
	Log     *slog.Logger
}

// Server renders the public pages.
type Server struct {
	store   *store.Store
	disk    *images.Disk
	baseURL string
	log     *slog.Logger
	tpl     map[string]*template.Template
}

// New parses the templates and returns a ready server.
func New(cfg Config) (*Server, error) {
	tpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{store: cfg.Store, disk: cfg.Disk, baseURL: cfg.BaseURL, log: cfg.Log, tpl: tpl}, nil
}

func parseTemplates() (map[string]*template.Template, error) {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	out := map[string]*template.Template{}
	for _, n := range names {
		base := path.Base(n)
		if base == "layout.html" {
			continue
		}
		t, err := template.New(base).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", n)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", base, err)
		}
		out[base] = t
	}
	return out, nil
}

// Register adds the public routes, /static/ and the catch-all 404 to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /media/paintings/{id}/{file}", s.media)
	mux.HandleFunc("GET /media/about/{file}", s.media)
	mux.Handle("GET /static/", assets.Handler())
	mux.HandleFunc("/", s.notFound)
}

// page is the data every template receives.
type page struct {
	Site         site.Info
	Title        string
	Description  string
	CanonicalURL string
	OGImage      string
	OGType       string
	Nav          string // "works", "about", "contacts"
	Data         any
}

func (s *Server) newPage(r *http.Request, info site.Info) page {
	return page{Site: info, Title: info.ArtistName, CanonicalURL: s.baseURL + r.URL.Path, OGType: "website"}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	t, ok := s.tpl[name]
	if !ok {
		s.log.Error("unknown template", "name", name)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", p); err != nil {
		s.log.Error("render failed", "template", name, "path", r.URL.Path, "err", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), s.store)
	if err != nil {
		s.log.Error("load site info", "err", err)
		info = site.Info{ArtistName: site.DefaultArtistName}
	}
	p := s.newPage(r, info)
	p.Title = "Страница не найдена — " + info.ArtistName
	s.render(w, r, http.StatusNotFound, "notfound.html", p)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	info, lerr := site.Load(r.Context(), s.store)
	if lerr != nil {
		info = site.Info{ArtistName: site.DefaultArtistName}
	}
	p := s.newPage(r, info)
	p.Title = "Ошибка — " + info.ArtistName
	s.render(w, r, http.StatusInternalServerError, "error.html", p)
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	dir := images.AboutDir
	if id := r.PathValue("id"); id != "" {
		dir = "paintings/" + id
	}
	file, ok := s.disk.VariantFile(dir, r.PathValue("file"))
	if !ok || !isFile(file) {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, file)
}
```

Also in `server.go`, add the helper (and `"os"` to imports):
```go
func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
```

- [ ] **Step 9: Write `home.go`**

`internal/web/home.go`:
```go
package web

import (
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

type homeData struct {
	Paintings []store.Painting
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	info, err := site.Load(ctx, s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	paintings, err := s.store.ListPaintings(ctx, true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	if info.Subtitle != "" {
		p.Title = info.ArtistName + " — " + info.Subtitle
	}
	p.Description = truncate(info.Greeting, 160)
	if p.Description == "" {
		p.Description = "Картины и истории их создания. " + info.ArtistName
	}
	if len(paintings) > 0 {
		first := paintings[0]
		p.OGImage = s.baseURL + imagesURL1200(first)
	}
	p.Nav = "works"
	p.Data = homeData{Paintings: paintings}
	s.render(w, r, http.StatusOK, "home.html", p)
}
```

Add to `funcs.go` (used by home and painting pages):
```go
func imagesURL1200(p store.Painting) string {
	return images.URL(images.PaintingDir(p.ID), p.ImageVersion, 1200)
}
```

- [ ] **Step 10: Write the templates**

`internal/web/templates/layout.html`:
```html
{{define "layout"}}<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
{{with .Description}}<meta name="description" content="{{.}}">{{end}}
<link rel="canonical" href="{{.CanonicalURL}}">
<meta property="og:type" content="{{.OGType}}">
<meta property="og:title" content="{{.Title}}">
{{with .Description}}<meta property="og:description" content="{{.}}">{{end}}
<meta property="og:url" content="{{.CanonicalURL}}">
<meta property="og:locale" content="ru_RU">
{{with .OGImage}}<meta property="og:image" content="{{.}}">
<meta name="twitter:card" content="summary_large_image">{{end}}
<link rel="stylesheet" href="/static/site.css">
</head>
<body>
<div class="wrap">
  <header class="hdr">
    <a class="brand" href="/">
      <span class="name">{{.Site.ArtistName}}</span>
      {{with .Site.Subtitle}}<span class="sub">{{.}}</span>{{end}}
    </a>
    <nav class="nav">
      <a href="/"{{if eq .Nav "works"}} class="cur" aria-current="page"{{end}}>Работы</a>
      <a href="/about"{{if eq .Nav "about"}} class="cur" aria-current="page"{{end}}>Об авторе</a>
      <a href="/contacts"{{if eq .Nav "contacts"}} class="cur" aria-current="page"{{end}}>Контакты</a>
    </nav>
  </header>
  <main>
{{template "content" .}}
  </main>
  {{with .Site.Contacts}}
  <footer class="foot">
    {{range .}}<a href="{{.Href}}">{{.Text}}</a>{{end}}
  </footer>
  {{end}}
</div>
{{block "scripts" .}}{{end}}
</body>
</html>
{{end}}
```

`internal/web/templates/home.html`:
```html
{{define "content"}}
{{with .Site.Greeting}}
<section class="intro">
  {{with $.Site.AboutPhoto}}<img class="intro-photo" src="{{imgURL "about" .Version 600}}" alt="">{{end}}
  <div>
    {{range paragraphs .}}<p>{{.}}</p>{{end}}
    <p><a href="/about">Подробнее обо мне →</a></p>
  </div>
</section>
{{end}}
{{with .Data.Paintings}}
<ul class="grid">
  {{range .}}
  <li class="card">
    <a href="/paintings/{{.Slug}}">
      <div class="mat">
        <img src="{{imgURL (paintingDir .ID) .ImageVersion 600}}"
             srcset="{{srcset (paintingDir .ID) .ImageVersion}}"
             sizes="(min-width: 1000px) 340px, (min-width: 640px) 45vw, 90vw"
             width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="{{.Title}}" loading="lazy">
      </div>
      <h2 class="t">{{.Title}}</h2>
      {{with details .}}<p class="m">{{.}}</p>{{end}}
    </a>
  </li>
  {{end}}
</ul>
{{else}}
<p class="empty">Скоро здесь появятся работы.</p>
{{end}}
{{end}}
```

`internal/web/templates/notfound.html`:
```html
{{define "content"}}
<section class="page">
  <h1>Страница не найдена</h1>
  <p>Возможно, ссылка устарела или в адресе опечатка.</p>
  <p><a href="/">← Ко всем работам</a></p>
</section>
{{end}}
```

`internal/web/templates/error.html`:
```html
{{define "content"}}
<section class="page">
  <h1>Что-то пошло не так</h1>
  <p>Попробуйте обновить страницу чуть позже.</p>
  <p><a href="/">← Ко всем работам</a></p>
</section>
{{end}}
```

- [ ] **Step 11: Run tests to verify they pass**

Run: `go test ./internal/web/ ./internal/assets/`
Expected: `ok` for `web` (`assets` has no tests: `no test files`)

- [ ] **Step 12: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/assets internal/web
git commit -m "feat: add public site skeleton with home page and media serving"
```

---

### Task 9: Painting page and lightbox

**Files:**
- Create: `internal/web/painting.go`
- Create: `internal/web/templates/painting.html`
- Create: `internal/assets/static/site.js`
- Modify: `internal/web/server.go` (`Register`: add the painting route)
- Test: `internal/web/painting_test.go`

**Interfaces:**
- Consumes: `newEnv`, `env.add`, `env.settings` (Task 8 test helpers); `truncate`, `details`, `imagesURL1200` (Task 8).
- Produces: route `GET /paintings/{slug}`; `/static/site.js` lightbox triggered by any `a[data-lightbox]`.

- [ ] **Step 1: Write the failing tests**

`internal/web/painting_test.go`:
```go
package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

func TestPaintingPage(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{site.KeyArtistName: "Анна"})
	p := e.add(gallery.PaintingInput{
		Title: "Карпы кои", Technique: "Бумага, акварель", Size: "21×30 см", Year: 2025, Visible: true,
		Description: "Первый абзац\nвторая строка\n\nВторой абзац <script>alert(1)</script>",
	})
	rec := e.get("/paintings/" + p.Slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<h1>Карпы кои</h1>",
		"Бумага, акварель · 21×30 см · 2025",
		"<p>Первый абзац<br>вторая строка</p>",
		"&lt;script&gt;",
		`href="/media/paintings/1/v1-2000.jpg"`,
		"data-lightbox",
		`<meta property="og:image" content="https://example.ru/media/paintings/1/v1-1200.jpg">`,
		`<meta property="og:type" content="article">`,
		"<title>Карпы кои — Анна</title>",
		`src="/static/site.js"`,
		"← Все работы",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("description must be escaped")
	}
}

func TestPaintingPageHiddenOrUnknownIs404(t *testing.T) {
	e := newEnv(t)
	hidden := e.add(gallery.PaintingInput{Title: "Скрытая", Visible: false})
	for _, path := range []string{"/paintings/" + hidden.Slug, "/paintings/net-takoy"} {
		if rec := e.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestPaintingPrevNextSkipHidden(t *testing.T) {
	e := newEnv(t)
	first := e.add(gallery.PaintingInput{Title: "Первая", Visible: true})
	e.add(gallery.PaintingInput{Title: "Скрытая", Visible: false})
	third := e.add(gallery.PaintingInput{Title: "Третья", Visible: true})
	// Site order (newest first): Третья, [Скрытая], Первая.

	top := e.get("/paintings/" + third.Slug).Body.String()
	if !strings.Contains(top, "Первая →") || strings.Contains(top, "Скрытая") {
		t.Errorf("top painting nav wrong:\n%s", top)
	}
	bottom := e.get("/paintings/" + first.Slug).Body.String()
	if !strings.Contains(bottom, "← Третья") || strings.Contains(bottom, "Скрытая") {
		t.Errorf("bottom painting nav wrong:\n%s", bottom)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run Painting`
Expected: FAIL — status 404 instead of 200 (route not registered)

- [ ] **Step 3: Write the handler**

`internal/web/painting.go`:
```go
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
```

In `internal/web/server.go` `Register`, add after the home route:
```go
	mux.HandleFunc("GET /paintings/{slug}", s.painting)
```

- [ ] **Step 4: Write the template**

`internal/web/templates/painting.html`:
```html
{{define "content"}}
{{with .Data.Painting}}
<a class="back" href="/">← Все работы</a>
<article class="work">
  <div class="image">
    <a class="mat zoom" href="{{imgURL (paintingDir .ID) .ImageVersion 2000}}" data-lightbox data-alt="{{.Title}}">
      <img src="{{imgURL (paintingDir .ID) .ImageVersion 1200}}"
           srcset="{{srcset (paintingDir .ID) .ImageVersion}}"
           sizes="(min-width: 900px) 600px, 95vw"
           width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="{{.Title}}">
    </a>
    <p class="hint">нажмите, чтобы открыть во весь экран</p>
  </div>
  <div class="text">
    <h1>{{.Title}}</h1>
    {{with details .}}<p class="meta">{{.}}</p>{{end}}
    {{with .Description}}
    <div class="story">
      {{range paragraphs .}}<p>{{range $i, $l := lines .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>{{end}}
    </div>
    {{end}}
    <nav class="pn">
      {{with $.Data.Prev}}<a href="/paintings/{{.Slug}}">← {{.Title}}</a>{{else}}<span></span>{{end}}
      {{with $.Data.Next}}<a href="/paintings/{{.Slug}}">{{.Title}} →</a>{{end}}
    </nav>
  </div>
</article>
{{end}}
{{end}}

{{define "scripts"}}<script src="/static/site.js" defer></script>{{end}}
```

- [ ] **Step 5: Write the lightbox script**

`internal/assets/static/site.js`:
```js
// Full-screen view of a painting. Without JS the link simply opens the image.
(function () {
  'use strict';
  var opener = document.querySelector('a[data-lightbox]');
  if (!opener) return;

  var box = document.createElement('div');
  box.className = 'lightbox';
  box.hidden = true;
  box.innerHTML = '<img alt=""><button type="button" class="lightbox-close" aria-label="Закрыть">×</button>';
  document.body.appendChild(box);
  var img = box.querySelector('img');

  function open(e) {
    e.preventDefault();
    img.src = opener.getAttribute('href');
    img.alt = opener.getAttribute('data-alt') || '';
    box.hidden = false;
    document.body.style.overflow = 'hidden';
  }

  function close() {
    box.hidden = true;
    document.body.style.overflow = '';
  }

  opener.addEventListener('click', open);
  box.addEventListener('click', close);
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !box.hidden) close();
  });
})();
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/web/`
Expected: `ok`

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/web internal/assets
git commit -m "feat: add painting page with lightbox and prev/next"
```

---

### Task 10: About, contacts, sitemap, robots

**Files:**
- Create: `internal/web/pages.go`
- Create: `internal/web/templates/about.html`, `internal/web/templates/contacts.html`
- Modify: `internal/web/server.go` (`Register`: add four routes)
- Test: `internal/web/pages_test.go`

**Interfaces:**
- Consumes: Task 8 helpers; `gallery.SetAboutPhoto` (Task 7, tests).
- Produces: routes `GET /about`, `GET /contacts`, `GET /sitemap.xml`, `GET /robots.txt`.

- [ ] **Step 1: Write the failing tests**

`internal/web/pages_test.go`:
```go
package web

import (
	"context"
	"image/color"
	"net/http"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestAboutWithTextAndPhoto(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{site.KeyAboutText: "Абзац один\n\nАбзац два"})
	if err := e.g.SetAboutPhoto(context.Background(), gallery.Photo{Original: testutil.JPEG(t, 300, 400, color.White)}); err != nil {
		t.Fatal(err)
	}
	rec := e.get("/about")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"<p>Абзац один</p>", "<p>Абзац два</p>", "/media/about/v1-1200.jpg", `class="cur" aria-current="page">Об авторе`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "text-only") {
		t.Error("page with photo must not use the text-only layout")
	}
}

func TestAboutEmpty(t *testing.T) {
	body := newEnv(t).get("/about").Body.String()
	if !strings.Contains(body, "Скоро здесь появится рассказ об авторе") || !strings.Contains(body, "text-only") {
		t.Errorf("unexpected body:\n%s", body)
	}
}

func TestContacts(t *testing.T) {
	e := newEnv(t)
	e.settings(map[string]string{site.KeyTelegram: "@anna_art", site.KeyEmail: "anna@example.ru"})
	body := e.get("/contacts").Body.String()
	for _, want := range []string{"Telegram", `href="https://t.me/anna_art"`, "@anna_art", `href="mailto:anna@example.ru"`, "Почта"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Error("a contact link was rejected by html/template")
	}
}

func TestContactsEmpty(t *testing.T) {
	if body := newEnv(t).get("/contacts").Body.String(); !strings.Contains(body, "Контакты скоро появятся") {
		t.Error("empty contacts text missing")
	}
}

func TestSitemap(t *testing.T) {
	e := newEnv(t)
	e.add(gallery.PaintingInput{Title: "Пион", Visible: true})
	e.add(gallery.PaintingInput{Title: "Скрытая", Visible: false})
	rec := e.get("/sitemap.xml")
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/xml") {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<loc>https://example.ru/</loc>", "<loc>https://example.ru/about</loc>",
		"<loc>https://example.ru/contacts</loc>", "<loc>https://example.ru/paintings/pion</loc>", "<lastmod>"} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap should contain %q", want)
		}
	}
	if strings.Contains(body, "skrytaya") {
		t.Error("hidden painting must not be in the sitemap")
	}
}

func TestRobots(t *testing.T) {
	body := newEnv(t).get("/robots.txt").Body.String()
	if !strings.Contains(body, "Disallow: /admin") || !strings.Contains(body, "Sitemap: https://example.ru/sitemap.xml") {
		t.Errorf("robots.txt = %q", body)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'About|Contacts|Sitemap|Robots'`
Expected: FAIL — 404 responses

- [ ] **Step 3: Write the handlers**

`internal/web/pages.go`:
```go
package web

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	p.Title = "Об авторе — " + info.ArtistName
	p.Description = truncate(info.AboutText, 160)
	if info.AboutPhoto != nil {
		p.OGImage = s.baseURL + images.URL(images.AboutDir, info.AboutPhoto.Version, 1200)
	}
	p.Nav = "about"
	s.render(w, r, http.StatusOK, "about.html", p)
}

func (s *Server) contacts(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), s.store)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, info)
	p.Title = "Контакты — " + info.ArtistName
	p.Description = "Как связаться с автором: " + info.ArtistName
	p.Nav = "contacts"
	s.render(w, r, http.StatusOK, "contacts.html", p)
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	paintings, err := s.store.ListPaintings(r.Context(), true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	set := sitemapSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, path := range []string{"/", "/about", "/contacts"} {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + path})
	}
	for _, p := range paintings {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/paintings/" + p.Slug, LastMod: p.UpdatedAt.Format("2006-01-02")})
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(set); err != nil {
		s.log.Error("write sitemap", "err", err)
	}
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin\n\nSitemap: %s/sitemap.xml\n", s.baseURL)
}
```

In `internal/web/server.go` `Register`, add after the painting route:
```go
	mux.HandleFunc("GET /about", s.about)
	mux.HandleFunc("GET /contacts", s.contacts)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /robots.txt", s.robots)
```

- [ ] **Step 4: Write the templates**

`internal/web/templates/about.html`:
```html
{{define "content"}}
<section class="page about{{if not .Site.AboutPhoto}} text-only{{end}}">
  {{with .Site.AboutPhoto}}
  <div class="mat">
    <img src="{{imgURL "about" .Version 1200}}" srcset="{{srcset "about" .Version}}"
         sizes="(min-width: 900px) 450px, 95vw" width="{{.Width}}" height="{{.Height}}" alt="{{$.Site.ArtistName}}">
  </div>
  {{end}}
  <div>
    <h1>Об авторе</h1>
    {{with .Site.AboutText}}
    <div class="story">
      {{range paragraphs .}}<p>{{range $i, $l := lines .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>{{end}}
    </div>
    {{else}}
    <p class="empty">Скоро здесь появится рассказ об авторе.</p>
    {{end}}
  </div>
</section>
{{end}}
```

`internal/web/templates/contacts.html`:
```html
{{define "content"}}
<section class="page">
  <h1>Контакты</h1>
  {{with .Site.Contacts}}
  <ul class="contacts">
    {{range .}}<li><span class="label">{{.Label}}</span><a href="{{.Href}}">{{.Text}}</a></li>{{end}}
  </ul>
  {{else}}
  <p class="empty">Контакты скоро появятся.</p>
  {{end}}
</section>
{{end}}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/web/`
Expected: `ok`

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/web
git commit -m "feat: add about, contacts, sitemap and robots"
```

---

### Task 11: Runnable server — main, logging, DEV seed content

**Files:**
- Create: `internal/seed/seed.go`
- Create: `internal/seed/photos/koi.jpg`, `lily.jpg`, `peony.jpg` (copied)
- Test: `internal/seed/seed_test.go`
- Create: `cmd/gallery/main.go`
- Create: `cmd/gallery/middleware.go`
- Test: `cmd/gallery/middleware_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `seed.Run(ctx context.Context, g *gallery.Gallery) error` — no-op if any painting exists
  - Binary `gallery` reading env `ADDR` (`:8080`), `DATA_DIR` (`./data`), `BASE_URL` (`http://localhost:8080`), `ADMIN_PASSWORD`, `DEV`, `TRUST_PROXY`; subcommand `gallery backup <dest.db>`
  - `logRequests(log *slog.Logger, next http.Handler) http.Handler`
  - In `main.go`: `type config struct { Addr, DataDir, BaseURL, AdminPassword string; Dev, TrustProxy bool }` and `func run(cfg config, log *slog.Logger) error` — Task 12 extends `run` with the admin.

- [ ] **Step 1: Copy the sample photos**

```bash
mkdir -p internal/seed/photos
cp .superpowers/mockups/1.jpg internal/seed/photos/koi.jpg
cp .superpowers/mockups/2.jpg internal/seed/photos/lily.jpg
cp .superpowers/mockups/3.jpg internal/seed/photos/peony.jpg
sips -g pixelWidth -g pixelHeight internal/seed/photos/*.jpg
```
Expected sizes: koi 1328×1920, lily 1365×1920, peony 1080×1891. (If `.superpowers/mockups/` is missing, ask the user for the three photos they shared in the brainstorming session.)

- [ ] **Step 2: Write the failing seed test**

`internal/seed/seed_test.go`:
```go
package seed

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

func TestRunSeedsOnceInOrder(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := &gallery.Gallery{Store: st, Disk: &images.Disk{Root: filepath.Join(dir, "images")}}
	ctx := context.Background()

	if err := Run(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, g); err != nil { // second run must be a no-op
		t.Fatal(err)
	}
	all, _ := st.ListPaintings(ctx, false)
	if len(all) != 3 || all[0].Title != "Карпы кои" || all[1].Title != "Жёлтая лилия" || all[2].Title != "Пион" {
		t.Fatalf("paintings = %+v", all)
	}
	if all[2].ImageWidth != 1080 || all[2].ImageHeight != 1640 {
		t.Errorf("peony should be cropped to 1080x1640, got %dx%d", all[2].ImageWidth, all[2].ImageHeight)
	}
	info, _ := site.Load(ctx, st)
	if info.ArtistName != "Имя Фамилия" {
		t.Errorf("ArtistName = %q", info.ArtistName)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/seed/`
Expected: FAIL — `undefined: Run`

- [ ] **Step 4: Write `seed.go`**

`internal/seed/seed.go`:
```go
// Package seed fills an empty database with sample content for local development.
package seed

import (
	"context"
	"embed"
	"fmt"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

//go:embed photos/*.jpg
var photos embed.FS

const sampleText = "Это пример описания. Здесь будет рассказ о картине: как появилась идея, что хотелось передать, какие материалы использованы.\n\nПустая строка начинает новый абзац."

type sample struct {
	file string
	in   gallery.PaintingInput
	crop images.Crop
}

// samples are in display order; the crops cut away the table, pen and camera stamp.
var samples = []sample{
	{"koi.jpg", gallery.PaintingInput{Title: "Карпы кои", Technique: "Бумага, акварель", Description: sampleText, Visible: true}, images.Crop{}},
	{"lily.jpg", gallery.PaintingInput{Title: "Жёлтая лилия", Technique: "Бумага, акварель", Description: sampleText, Visible: true}, images.Crop{X: 0, Y: 0, W: 1330, H: 1920}},
	{"peony.jpg", gallery.PaintingInput{Title: "Пион", Technique: "Бумага, акварель", Year: 2025, Description: sampleText, Visible: true}, images.Crop{X: 0, Y: 60, W: 1080, H: 1640}},
}

// Run adds sample settings and paintings if there are no paintings yet.
func Run(ctx context.Context, g *gallery.Gallery) error {
	existing, err := g.Store.ListPaintings(ctx, false)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if err := g.Store.SetSettings(ctx, map[string]string{
		site.KeyArtistName: "Имя Фамилия",
		site.KeySubtitle:   "художник, акварель",
		site.KeyGreeting:   "Здравствуйте! Я пишу акварелью цветы, воду и свет. Здесь собраны мои работы — каждая со своей историей.",
		site.KeyAboutText:  "Здесь будет рассказ об авторе.\n\nЕго можно изменить в разделе «Об авторе» в управлении сайтом.",
		site.KeyEmail:      "mail@example.ru",
		site.KeyTelegram:   "example",
	}); err != nil {
		return err
	}
	for i := len(samples) - 1; i >= 0; i-- { // each new painting goes on top
		s := samples[i]
		data, err := photos.ReadFile("photos/" + s.file)
		if err != nil {
			return err
		}
		if _, err := g.AddPainting(ctx, s.in, gallery.Photo{Original: data, Crop: s.crop}); err != nil {
			return fmt.Errorf("seed %s: %w", s.file, err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the seed test**

Run: `go test ./internal/seed/`
Expected: `ok`

- [ ] **Step 6: Write the failing middleware tests**

`cmd/gallery/middleware_test.go`:
```go
package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogRequestsRecordsStatus(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h := logRequests(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	out := buf.String()
	if !strings.Contains(out, "status=418") || !strings.Contains(out, "path=/x") || !strings.Contains(out, "method=GET") {
		t.Fatalf("log = %q", out)
	}
}

func TestLogRequestsRecoversPanics(t *testing.T) {
	var buf bytes.Buffer
	h := logRequests(slog.New(slog.NewTextHandler(&buf, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("panic not logged: %q", buf.String())
	}
}
```

- [ ] **Step 7: Write `middleware.go`**

`cmd/gallery/middleware.go`:
```go
package main

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests logs every request and turns panics into a 500 response.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic", "err", v, "method", r.Method, "path", r.URL.Path)
				http.Error(rec, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			}
			log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
		}()
		next.ServeHTTP(rec, r)
	})
}
```

- [ ] **Step 8: Write `main.go`**

`cmd/gallery/main.go`:
```go
// Command gallery runs the painter's portfolio website.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/seed"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/web"
)

type config struct {
	Addr          string
	DataDir       string
	BaseURL       string
	AdminPassword string
	Dev           bool
	TrustProxy    bool
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() config {
	return config{
		Addr:          env("ADDR", ":8080"),
		DataDir:       env("DATA_DIR", "./data"),
		BaseURL:       strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		Dev:           os.Getenv("DEV") == "1",
		TrustProxy:    os.Getenv("TRUST_PROXY") == "1",
	}
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := loadConfig()

	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: gallery backup <dest.db>")
			os.Exit(2)
		}
		if err := backup(cfg, os.Args[2]); err != nil {
			log.Error("backup failed", "err", err)
			os.Exit(1)
		}
		return
	}

	if err := run(cfg, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func dbPath(cfg config) string { return filepath.Join(cfg.DataDir, "gallery.db") }

func backup(cfg config, dest string) error {
	st, err := store.Open(dbPath(cfg))
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Backup(context.Background(), dest)
}

func run(cfg config, log *slog.Logger) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(dbPath(cfg))
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	disk := &images.Disk{Root: filepath.Join(cfg.DataDir, "images")}
	g := &gallery.Gallery{Store: st, Disk: disk}
	if cfg.Dev {
		if err := seed.Run(ctx, g); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}

	public, err := web.New(web.Config{Store: st, Disk: disk, BaseURL: cfg.BaseURL, Log: log})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	public.Register(mux)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           logRequests(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "dev", cfg.Dev)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

- [ ] **Step 9: Run the middleware tests**

Run: `go test ./cmd/gallery/`
Expected: `ok`

- [ ] **Step 10: Run all tests**

Run: `go test ./...`
Expected: all packages `ok`

- [ ] **Step 11: Manual check of the public site**

Start the server in the background:
```bash
DEV=1 go run ./cmd/gallery
```
Open `http://localhost:8080` in the browser pane. Check on desktop width and with the mobile preset (375×812):
- Home: handwritten name, subtitle, greeting block, three paintings in matted cards (3 columns desktop, 1 column phone), captions, footer contacts. Compare with option C in `.superpowers/mockups/visual-style.html`.
- Painting page: side-by-side on desktop with the image sticky, stacked on phone; peony has no pen/table/stamp; tapping the image opens the lightbox, Esc/tap closes; prev/next links work. Compare with option A in `.superpowers/mockups/painting-page.html`.
- `/about`, `/contacts`, `/sitemap.xml`, `/robots.txt`, `/nope` (Russian 404).
- `curl -s localhost:8080/ | grep -c fonts.googleapis` → `0` (no external resources).

Fix anything that looks broken before committing. Stop the server afterwards. `data/` is gitignored.

- [ ] **Step 12: Commit**

```bash
gofmt -l . && go vet ./...
git add cmd internal/seed
git commit -m "feat: add server entrypoint, request logging and DEV sample content"
```

---

### Task 12: Admin foundation — login, sessions, CSRF, rate limit

**Files:**
- Create: `internal/admin/admin.go`
- Create: `internal/admin/auth.go`
- Create: `internal/admin/templates/layout.html`, `login.html`, `message.html`
- Create: `internal/assets/static/admin.css`
- Modify: `cmd/gallery/main.go` (wire the admin)
- Test: `internal/admin/helpers_test.go`, `internal/admin/auth_test.go`

**Interfaces:**
- Consumes: `gallery.Gallery` (Task 7), `store` sessions/settings (Task 5), `images.URL/PaintingDir` (Task 3).
- Produces (used by Tasks 13–16):
  - `type admin.Config struct { Gallery *gallery.Gallery; Log *slog.Logger; SecureCookies, TrustProxy bool; Now func() time.Time }`
  - `func admin.New(cfg Config) (*Admin, error)`, `func (a *Admin) Register(mux *http.ServeMux)`
  - `func admin.EnsurePassword(ctx context.Context, st *store.Store, initial string) error`
  - Inside the package: `a.g` (gallery), `a.st` (store), `a.log`, `a.now()`; `a.requireAuth(h http.HandlerFunc) http.HandlerFunc` (session check; for POST also body limit + multipart parse + CSRF check from form field `csrf` or header `X-CSRF-Token`); `type view struct { Title, CSRF, Nav, Flash string; Data any }`; `a.render(w, r, status, "page.html", view)`; `a.renderMessage(w, r, status, title, msg)`; `a.notFound(w, r)`; `a.serverError(w, r, err)`; `noCache(w)`; `checkPassword(ctx, st, pw) (bool, error)`; `setPassword(ctx, st, pw) error`; constants `maxPhotoBytes`, `maxRequestBytes`, `cookieName`
  - Templates: files named `_*.html` are partials parsed into every page; every page defines `content` and optionally `head` / `scripts`
  - Flash messages from `?msg=`: `saved` → «Сохранено», `deleted` → «Картина удалена», `password` → «Пароль изменён»
  - Test helpers: `newHarness(t, opts ...func(*Config)) *harness`, `h.get(path, cookie)`, `h.postForm(path, url.Values, cookie)`, `h.do(req, cookie)`, `h.login() (*http.Cookie, string /*csrf*/)`, `testPassword`

- [ ] **Step 1: Add bcrypt**

```bash
go get golang.org/x/crypto/bcrypt
```

- [ ] **Step 2: Write the test helpers**

`internal/admin/helpers_test.go`:
```go
package admin

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

const testPassword = "correct horse battery"

type harness struct {
	t   *testing.T
	st  *store.Store
	g   *gallery.Gallery
	h   http.Handler
	now time.Time
}

func newHarness(t *testing.T, opts ...func(*Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &gallery.Gallery{Store: st, Disk: &images.Disk{Root: filepath.Join(dir, "images")}}
	if err := EnsurePassword(context.Background(), st, testPassword); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, st: st, g: g, now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	cfg := Config{Gallery: g, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return h.now }}
	for _, o := range opts {
		o(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.Register(mux)
	h.h = mux
	return h
}

func (h *harness) do(req *http.Request, c *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func (h *harness) get(path string, c *http.Cookie) *httptest.ResponseRecorder {
	return h.do(httptest.NewRequest(http.MethodGet, path, nil), c)
}

func (h *harness) postForm(path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return h.do(req, c)
}

// login signs in and returns the session cookie and the CSRF token.
func (h *harness) login() (*http.Cookie, string) {
	h.t.Helper()
	rec := h.postForm("/admin/login", url.Values{"password": {testPassword}}, nil)
	if rec.Code != http.StatusSeeOther {
		h.t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c, csrfFor(c.Value)
		}
	}
	h.t.Fatal("no session cookie")
	return nil, ""
}

func location(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Location") }
```

- [ ] **Step 3: Write the failing auth tests**

`internal/admin/auth_test.go`:
```go
package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

func TestEnsurePassword(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Already set by the harness: a different initial password must not replace it.
	if err := EnsurePassword(ctx, h.st, "something else"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := checkPassword(ctx, h.st, testPassword); !ok {
		t.Fatal("original password must still work")
	}
}

func TestEnsurePasswordRequiresInitialValue(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/x.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := EnsurePassword(context.Background(), st, ""); err == nil {
		t.Fatal("expected an error when no password is stored and none is given")
	}
}

func TestLoginPage(t *testing.T) {
	rec := newHarness(t).get("/admin/login", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Вход") {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestProtectedPagesRedirectToLogin(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/admin/", "/admin/whatever"} {
		rec := h.get(path, nil)
		if rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
			t.Errorf("%s: status = %d, location = %q", path, rec.Code, location(rec))
		}
	}
}

func TestLoginWrongPassword(t *testing.T) {
	rec := newHarness(t).postForm("/admin/login", url.Values{"password": {"nope"}}, nil)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Неверный пароль") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("no cookie on failed login")
	}
}

func TestLoginSuccess(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	if !c.HttpOnly || c.Path != "/admin" || c.SameSite != http.SameSiteLaxMode || c.Secure {
		t.Errorf("cookie = %+v", c)
	}
	rec := h.get("/admin/", c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	if rec := h.get("/admin/login", c); location(rec) != "/admin/paintings" {
		t.Errorf("logged-in user should be sent from login to paintings, got %q", location(rec))
	}
}

func TestSecureCookies(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SecureCookies = true })
	c, _ := h.login()
	if !c.Secure {
		t.Fatal("cookie must be Secure")
	}
}

func TestSessionExpires(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	h.now = h.now.Add(31 * 24 * time.Hour)
	if rec := h.get("/admin/", c); location(rec) != "/admin/login" {
		t.Fatalf("expired session: location = %q", location(rec))
	}
}

func TestUnknownAdminPageIs404WhenLoggedIn(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	rec := h.get("/admin/nope", c)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Страница не найдена") {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestLogoutRequiresCSRF(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()

	rec := h.postForm("/admin/logout", url.Values{}, c)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Страница устарела") {
		t.Fatalf("without csrf: status = %d", rec.Code)
	}

	rec = h.postForm("/admin/logout", url.Values{"csrf": {csrf}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
		t.Fatalf("with csrf: status = %d, location = %q", rec.Code, location(rec))
	}
	if rec := h.get("/admin/", c); location(rec) != "/admin/login" {
		t.Fatal("session must be gone after logout")
	}
}

func TestCSRFHeaderIsAccepted(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	req.Header.Set("X-CSRF-Token", csrf)
	if rec := h.do(req, c); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 5; i++ {
		if rec := h.postForm("/admin/login", url.Values{"password": {"wrong"}}, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d", i+1, rec.Code)
		}
	}
	rec := h.postForm("/admin/login", url.Values{"password": {testPassword}}, nil)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Слишком много попыток") {
		t.Fatalf("6th attempt: status = %d", rec.Code)
	}
	h.now = h.now.Add(16 * time.Minute)
	h.login() // works again
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:5555"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if got := clientIP(req, false); got != "10.0.0.2" {
		t.Errorf("untrusted: got %q", got)
	}
	if got := clientIP(req, true); got != "203.0.113.9" {
		t.Errorf("trusted: got %q", got)
	}
	req.Header.Del("X-Forwarded-For")
	if got := clientIP(req, true); got != "10.0.0.2" {
		t.Errorf("trusted without header: got %q", got)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/admin/`
Expected: FAIL — `undefined: EnsurePassword` (and others)

- [ ] **Step 5: Write `admin.go`**

`internal/admin/admin.go`:
```go
// Package admin serves the content-management pages under /admin.
package admin

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

const (
	maxPhotoBytes   = 30 << 20
	maxRequestBytes = maxPhotoBytes + 2<<20
)

// Config holds the admin's dependencies.
type Config struct {
	Gallery       *gallery.Gallery
	Log           *slog.Logger
	SecureCookies bool             // false only for local development over http
	TrustProxy    bool             // take the client IP from X-Forwarded-For
	Now           func() time.Time // defaults to time.Now
}

// Admin serves /admin.
type Admin struct {
	g          *gallery.Gallery
	st         *store.Store
	log        *slog.Logger
	secure     bool
	trustProxy bool
	now        func() time.Time
	limiter    *limiter
	tpl        map[string]*template.Template
}

// New parses the templates and returns a ready admin.
func New(cfg Config) (*Admin, error) {
	tpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Admin{
		g: cfg.Gallery, st: cfg.Gallery.Store, log: cfg.Log,
		secure: cfg.SecureCookies, trustProxy: cfg.TrustProxy, now: now,
		limiter: newLimiter(5, 15*time.Minute), tpl: tpl,
	}, nil
}

var funcs = template.FuncMap{
	"imgURL":      images.URL,
	"paintingDir": images.PaintingDir,
}

func parseTemplates() (map[string]*template.Template, error) {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	var partials []string
	for _, n := range names {
		if strings.HasPrefix(path.Base(n), "_") {
			partials = append(partials, n)
		}
	}
	out := map[string]*template.Template{}
	for _, n := range names {
		base := path.Base(n)
		if base == "layout.html" || strings.HasPrefix(base, "_") {
			continue
		}
		files := append(append([]string{"templates/layout.html"}, partials...), n)
		t, err := template.New(base).Funcs(funcs).ParseFS(templateFS, files...)
		if err != nil {
			return nil, fmt.Errorf("admin: parse %s: %w", base, err)
		}
		out[base] = t
	}
	return out, nil
}

// Register adds the admin routes to mux.
func (a *Admin) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/login", a.loginPage)
	mux.HandleFunc("POST /admin/login", a.login)
	mux.HandleFunc("POST /admin/logout", a.requireAuth(a.logout))
	mux.HandleFunc("GET /admin/{$}", a.requireAuth(a.index))
	mux.HandleFunc("/admin/", a.requireAuth(a.notFound))
}

// view is the data every admin template receives.
type view struct {
	Title string
	CSRF  string // empty on pages shown to logged-out users; hides the menu
	Nav   string // "paintings", "about", "settings"
	Flash string
	Data  any
}

var flashes = map[string]string{
	"saved":    "Сохранено",
	"deleted":  "Картина удалена",
	"password": "Пароль изменён",
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
}

func (a *Admin) render(w http.ResponseWriter, r *http.Request, status int, name string, v view) {
	if v.CSRF == "" {
		v.CSRF = csrfToken(r)
	}
	if v.Flash == "" {
		v.Flash = flashes[r.URL.Query().Get("msg")]
	}
	t, ok := a.tpl[name]
	if !ok {
		a.log.Error("unknown template", "name", name)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		a.log.Error("render failed", "template", name, "err", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	noCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *Admin) renderMessage(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	a.render(w, r, status, "message.html", view{Title: title, Data: msg})
}

func (a *Admin) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderMessage(w, r, http.StatusNotFound, "Страница не найдена", "Такой страницы нет.")
}

func (a *Admin) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("admin request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	a.renderMessage(w, r, http.StatusInternalServerError, "Ошибка", "Что-то пошло не так. Попробуйте ещё раз чуть позже.")
}

func (a *Admin) index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/paintings", http.StatusSeeOther)
}
```

- [ ] **Step 6: Write `auth.go`**

`internal/admin/auth.go`:
```go
package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

const (
	cookieName  = "gallery_session"
	sessionTTL  = 30 * 24 * time.Hour
	passwordKey = "password_hash"
)

type ctxKey struct{}

// csrfToken returns the CSRF token of the logged-in request, or "".
func csrfToken(r *http.Request) string {
	v, _ := r.Context().Value(ctxKey{}).(string)
	return v
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// csrfFor derives the per-session CSRF token from the raw session token.
func csrfFor(raw string) string {
	sum := sha256.Sum256([]byte("csrf:" + raw))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EnsurePassword stores the initial admin password if none is stored yet.
func EnsurePassword(ctx context.Context, st *store.Store, initial string) error {
	hash, err := st.Setting(ctx, passwordKey)
	if err != nil {
		return err
	}
	if hash != "" {
		return nil
	}
	if initial == "" {
		return errors.New("admin: ADMIN_PASSWORD must be set on first start")
	}
	return setPassword(ctx, st, initial)
}

func setPassword(ctx context.Context, st *store.Store, pw string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return st.SetSettings(ctx, map[string]string{passwordKey: string(hash)})
}

func checkPassword(ctx context.Context, st *store.Store, pw string) (bool, error) {
	hash, err := st.Setting(ctx, passwordKey)
	if err != nil {
		return false, err
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil, nil
}

// session returns the raw session token if the request has a valid session.
func (a *Admin) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	ok, err := a.st.SessionValid(r.Context(), hashToken(c.Value), a.now())
	if err != nil {
		a.log.Error("check session", "err", err)
		return "", false
	}
	return c.Value, ok
}

// requireAuth lets only logged-in users through. For POST requests it also
// limits the body size, parses the form and checks the CSRF token.
func (a *Admin) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		raw, ok := a.session(r)
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		csrf := csrfFor(raw)
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
			got := r.Header.Get("X-CSRF-Token")
			if got == "" {
				if err := r.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
					var tooBig *http.MaxBytesError
					if errors.As(err, &tooBig) {
						a.renderMessage(w, r, http.StatusRequestEntityTooLarge, "Фото слишком большое",
							"Максимальный размер фото — 30 МБ. Вернитесь назад и выберите другое фото.")
						return
					}
					a.renderMessage(w, r, http.StatusBadRequest, "Ошибка формы", "Не удалось прочитать форму. Вернитесь назад и попробуйте ещё раз.")
					return
				}
				got = r.PostFormValue("csrf")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(csrf)) != 1 {
				a.renderMessage(w, r, http.StatusForbidden, "Страница устарела",
					"Страница устарела. Вернитесь назад, обновите её и попробуйте снова.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, csrf)))
	}
}

type loginData struct {
	Error string
}

func (a *Admin) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.session(r); ok {
		http.Redirect(w, r, "/admin/paintings", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "login.html", view{Title: "Вход", Data: loginData{}})
}

func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	ctx := r.Context()
	ip := clientIP(r, a.trustProxy)
	now := a.now()
	if !a.limiter.Allow(ip, now) {
		a.render(w, r, http.StatusTooManyRequests, "login.html",
			view{Title: "Вход", Data: loginData{"Слишком много попыток. Попробуйте через 15 минут."}})
		return
	}
	ok, err := checkPassword(ctx, a.st, r.PostFormValue("password"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !ok {
		a.limiter.Fail(ip, now)
		a.render(w, r, http.StatusUnauthorized, "login.html", view{Title: "Вход", Data: loginData{"Неверный пароль"}})
		return
	}
	a.limiter.Reset(ip)
	raw, err := newToken()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.st.DeleteExpiredSessions(ctx, now); err != nil {
		a.log.Error("delete expired sessions", "err", err)
	}
	if err := a.st.CreateSession(ctx, hashToken(raw), now.Add(sessionTTL)); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: raw, Path: "/admin",
		Expires: now.Add(sessionTTL), MaxAge: int(sessionTTL / time.Second),
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/paintings", http.StatusSeeOther)
}

func (a *Admin) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := a.st.DeleteSession(r.Context(), hashToken(c.Value)); err != nil {
			a.log.Error("delete session", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/admin", MaxAge: -1,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// clientIP returns the visitor's IP. Behind Caddy (trustProxy) it is the
// last X-Forwarded-For entry, which Caddy sets itself.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter counts failed logins per IP in a sliding window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, fails: map[string][]time.Time{}}
}

func (l *limiter) recent(ip string, now time.Time) []time.Time {
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, ip)
		return nil
	}
	l.fails[ip] = kept
	return kept
}

// Allow reports whether another login attempt from ip is permitted.
func (l *limiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, now)) < l.max
}

// Fail records a failed attempt.
func (l *limiter) Fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[ip] = append(l.recent(ip, now), now)
}

// Reset forgets failures after a successful login.
func (l *limiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
```

- [ ] **Step 7: Write the templates**

`internal/admin/templates/layout.html`:
```html
{{define "layout"}}<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}} · Управление сайтом</title>
<link rel="stylesheet" href="/static/admin.css">
{{block "head" .}}{{end}}
</head>
<body>
{{if .CSRF}}
<header class="bar">
  <nav>
    <a href="/admin/paintings"{{if eq .Nav "paintings"}} class="cur"{{end}}>Картины</a>
    <a href="/admin/about"{{if eq .Nav "about"}} class="cur"{{end}}>Об авторе</a>
    <a href="/admin/settings"{{if eq .Nav "settings"}} class="cur"{{end}}>Настройки</a>
  </nav>
  <div class="bar-right">
    <a href="/" target="_blank" rel="noopener">Открыть сайт ↗</a>
    <form method="post" action="/admin/logout">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <button class="link">Выйти</button>
    </form>
  </div>
</header>
{{end}}
<main class="box">
  {{with .Flash}}<p class="flash">{{.}}</p>{{end}}
{{template "content" .}}
</main>
{{block "scripts" .}}{{end}}
</body>
</html>
{{end}}
```

`internal/admin/templates/login.html`:
```html
{{define "content"}}
<h1>Вход</h1>
<form method="post" action="/admin/login" class="form narrow">
  {{with .Data.Error}}<p class="error">{{.}}</p>{{end}}
  <label>Пароль
    <input type="password" name="password" autocomplete="current-password" required autofocus>
  </label>
  <button class="primary">Войти</button>
</form>
{{end}}
```

`internal/admin/templates/message.html`:
```html
{{define "content"}}
<h1>{{.Title}}</h1>
<p>{{.Data}}</p>
<p><a href="/admin/paintings">← К списку картин</a></p>
{{end}}
```

- [ ] **Step 8: Write `admin.css`**

`internal/assets/static/admin.css`:
```css
[hidden] { display: none !important; }
* { box-sizing: border-box; }
body { margin: 0; background: #f5f3ef; color: #2b2622; font: 17px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
a { color: #9c4a2f; }

.bar { position: sticky; top: 0; z-index: 5; display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 8px 16px; padding: 8px 12px; background: #fff; border-bottom: 1px solid #e3ddd3; }
.bar nav { display: flex; flex-wrap: wrap; gap: 4px; }
.bar nav a { padding: 10px 12px; border-radius: 8px; color: #4a4038; text-decoration: none; }
.bar nav a.cur { background: #f1e7dc; color: #9c4a2f; font-weight: 600; }
.bar-right { display: flex; align-items: center; gap: 14px; font-size: 15px; }
.bar-right form { margin: 0; }

.box { max-width: 860px; margin: 0 auto; padding: 20px 16px 80px; }
h1 { margin: 8px 0 20px; font-size: 26px; }
.page-head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 12px; }
.page-head h1 { margin: 8px 0; }
.flash { padding: 12px 14px; border: 1px solid #b9d8b0; border-radius: 8px; background: #e6f2e2; }
.error { margin: 4px 0 0; color: #b3261e; font-size: 15px; font-weight: 400; }
.hint { margin: 0; color: #7a6f64; font-size: 15px; }

.form { display: grid; gap: 18px; }
.form.narrow { max-width: 360px; }
label { display: grid; gap: 6px; font-weight: 600; }
label small { color: #7a6f64; font-weight: 400; }
input[type=text], input[type=password], input[type=number], input[type=email], input[type=tel], textarea {
  width: 100%; padding: 12px; border: 1px solid #cfc6b8; border-radius: 8px; background: #fff; font: inherit;
}
textarea { min-height: 220px; resize: vertical; }
textarea.short { min-height: 110px; }
.check { display: flex; align-items: center; gap: 10px; }
.check input { width: 22px; height: 22px; }
fieldset { display: grid; gap: 14px; margin: 0; padding: 16px; border: 1px solid #e3ddd3; border-radius: 10px; background: #fff; }
legend { padding: 0 6px; font-weight: 700; }

button, .button { display: inline-block; padding: 12px 18px; border: 1px solid #cfc6b8; border-radius: 8px; background: #fff; color: #2b2622; font: inherit; text-align: center; text-decoration: none; cursor: pointer; }
.primary { border-color: #9c4a2f; background: #9c4a2f; color: #fff; font-weight: 600; }
.danger { border-color: #b3261e; color: #b3261e; }
button.link { padding: 0; border: 0; background: none; color: #9c4a2f; text-decoration: underline; }
.actions { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
.danger-zone { margin-top: 40px; padding-top: 20px; border-top: 1px solid #e3ddd3; }

.list { list-style: none; margin: 0; padding: 0; display: grid; gap: 10px; }
.list li { display: flex; align-items: center; gap: 12px; padding: 8px 12px 8px 4px; border: 1px solid #e3ddd3; border-radius: 10px; background: #fff; }
.list img { flex: none; width: 64px; height: 64px; border-radius: 6px; object-fit: cover; }
.list .title { flex: 1; min-width: 0; color: inherit; font-weight: 600; text-decoration: none; }
.badge { margin-left: 6px; padding: 2px 8px; border-radius: 99px; background: #eee6da; color: #6b5c4d; font-size: 13px; font-weight: 400; }
.drag { padding: 10px; color: #a3988b; font-size: 22px; line-height: 1; cursor: grab; user-select: none; touch-action: none; }
.sortable-ghost { opacity: .4; }
.status { min-height: 1.5em; color: #4f7a43; font-size: 15px; }

.photo-current img { width: auto; max-width: 100%; max-height: 320px; border-radius: 6px; }
.crop-stage { height: 60vh; min-height: 280px; overflow: hidden; border-radius: 8px; background: #e9e4dc; }
.crop-stage img { display: block; max-width: 100%; }
.crop-tools { display: flex; flex-wrap: wrap; gap: 8px; }
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go mod tidy && go test ./internal/admin/`
Expected: `ok`

- [ ] **Step 10: Wire the admin into `main.go`**

In `cmd/gallery/main.go`, add the import `"github.com/lilxtent/pictures-gallery/internal/admin"` and, in `run`, replace

```go
	mux := http.NewServeMux()
	public.Register(mux)
```

with

```go
	if err := admin.EnsurePassword(ctx, st, cfg.AdminPassword); err != nil {
		return err
	}
	adm, err := admin.New(admin.Config{Gallery: g, Log: log, SecureCookies: !cfg.Dev, TrustProxy: cfg.TrustProxy})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	public.Register(mux)
	adm.Register(mux)
```

- [ ] **Step 11: Verify the whole build and a manual login**

Run: `go vet ./... && go test ./...`
Expected: all `ok`.

Then start `DEV=1 ADMIN_PASSWORD=dev-password go run ./cmd/gallery`, open `http://localhost:8080/admin` in the browser pane, log in with `dev-password` (the local development password documented in the README in Task 17), confirm you land on `/admin/paintings` (404 page with the admin menu until Task 13) and that «Выйти» logs out. Stop the server.

- [ ] **Step 12: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod go.sum internal/admin internal/assets cmd
git commit -m "feat: add admin login with sessions, CSRF and rate limiting"
```

---

### Task 13: Admin paintings list with drag-to-reorder

**Files:**
- Create: `internal/admin/paintings.go`
- Create: `internal/admin/templates/paintings.html`
- Create: `internal/assets/static/admin.js` (reorder only; Task 14 replaces the whole file)
- Create: `internal/assets/static/vendor/Sortable.min.js` (downloaded)
- Modify: `internal/admin/admin.go` (`Register`)
- Test: `internal/admin/paintings_test.go`

**Interfaces:**
- Consumes: Task 12 (`requireAuth`, `render`, harness).
- Produces: `GET /admin/paintings`; `POST /admin/paintings/reorder` taking JSON `{"ids":[3,1,2]}` with header `X-CSRF-Token`, answering `204`.

- [ ] **Step 1: Vendor SortableJS**

```bash
mkdir -p internal/assets/static/vendor
curl -fsSLo internal/assets/static/vendor/Sortable.min.js https://cdnjs.cloudflare.com/ajax/libs/Sortable/1.15.2/Sortable.min.js
head -c 120 internal/assets/static/vendor/Sortable.min.js
```
Expected: starts with `/*! Sortable 1.15.2`.

- [ ] **Step 2: Write the failing tests**

`internal/admin/paintings_test.go`:
```go
package admin

import (
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func (h *harness) addPainting(title string, visible bool) store.Painting {
	h.t.Helper()
	p, err := h.g.AddPainting(context.Background(), gallery.PaintingInput{Title: title, Visible: visible},
		gallery.Photo{Original: testutil.JPEG(h.t, 300, 200, color.White)})
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func TestListRequiresLogin(t *testing.T) {
	if rec := newHarness(t).get("/admin/paintings", nil); location(rec) != "/admin/login" {
		t.Fatalf("location = %q", location(rec))
	}
}

func TestListShowsPaintings(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	h.addPainting("Скрытая", false)
	c, csrf := h.login()
	rec := h.get("/admin/paintings?msg=saved", c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Пион", "Скрытая", "Скрыта</span>", `href="/admin/paintings/1"`,
		`data-csrf="` + csrf + `"`, "+ Добавить картину", "Сохранено", "/static/vendor/Sortable.min.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestListEmpty(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	if body := h.get("/admin/paintings", c).Body.String(); !strings.Contains(body, "Пока нет ни одной картины") {
		t.Fatal("empty state missing")
	}
}

func TestReorder(t *testing.T) {
	h := newHarness(t)
	a := h.addPainting("A", true)
	b := h.addPainting("B", true)
	c, csrf := h.login()

	req := httptest.NewRequest(http.MethodPost, "/admin/paintings/reorder", strings.NewReader(`{"ids":[`+itoa(a.ID)+`,`+itoa(b.ID)+`]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	if rec := h.do(req, c); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	all, _ := h.st.ListPaintings(context.Background(), false)
	if all[0].Title != "A" || all[1].Title != "B" {
		t.Fatalf("order = %s, %s", all[0].Title, all[1].Title)
	}
}

func TestReorderRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	for _, body := range []string{`not json`, `{"ids":[]}`} {
		req := httptest.NewRequest(http.MethodPost, "/admin/paintings/reorder", strings.NewReader(body))
		req.Header.Set("X-CSRF-Token", csrf)
		if rec := h.do(req, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", body, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/paintings/reorder", strings.NewReader(`{"ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	if rec := h.do(req, c); rec.Code != http.StatusForbidden {
		t.Errorf("without CSRF: status = %d, want 403", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```
(add `"strconv"` to the imports.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/admin/ -run 'List|Reorder'`
Expected: FAIL — 404 / wrong status (routes missing)

- [ ] **Step 4: Write the handlers**

`internal/admin/paintings.go`:
```go
package admin

import (
	"encoding/json"
	"net/http"
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
```

In `internal/admin/admin.go` `Register`, add before the catch-all `/admin/` line:
```go
	mux.HandleFunc("GET /admin/paintings", a.requireAuth(a.list))
	mux.HandleFunc("POST /admin/paintings/reorder", a.requireAuth(a.reorder))
```

- [ ] **Step 5: Write the template**

`internal/admin/templates/paintings.html`:
```html
{{define "content"}}
<div class="page-head">
  <h1>Картины</h1>
  <a class="button primary" href="/admin/paintings/new">+ Добавить картину</a>
</div>
{{with .Data}}
<p class="hint">Перетащите картину за значок ⠿, чтобы изменить порядок на сайте.</p>
<ul class="list" data-sortable data-csrf="{{$.CSRF}}">
  {{range .}}
  <li data-id="{{.ID}}">
    <span class="drag" aria-hidden="true">⠿</span>
    <img src="{{imgURL (paintingDir .ID) .ImageVersion 600}}" alt="">
    <a class="title" href="/admin/paintings/{{.ID}}">{{.Title}}{{if not .Visible}}<span class="badge">Скрыта</span>{{end}}</a>
  </li>
  {{end}}
</ul>
<p class="status" data-sort-status></p>
{{else}}
<p>Пока нет ни одной картины. Нажмите «Добавить картину».</p>
{{end}}
{{end}}

{{define "scripts"}}
<script src="/static/vendor/Sortable.min.js"></script>
<script src="/static/admin.js"></script>
{{end}}
```

- [ ] **Step 6: Write `admin.js` (reorder part)**

`internal/assets/static/admin.js`:
```js
// Admin helpers. Task 14 extends this file with photo cropping.
(function () {
  'use strict';

  // Drag-to-reorder on the paintings list.
  var list = document.querySelector('[data-sortable]');
  if (list && window.Sortable) {
    var status = document.querySelector('[data-sort-status]');
    Sortable.create(list, {
      handle: '.drag',
      animation: 150,
      onEnd: function () {
        var ids = Array.prototype.map.call(list.querySelectorAll('[data-id]'), function (li) {
          return Number(li.getAttribute('data-id'));
        });
        status.textContent = 'Сохраняю…';
        fetch('/admin/paintings/reorder', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': list.getAttribute('data-csrf') },
          body: JSON.stringify({ ids: ids })
        }).then(function (r) {
          status.textContent = r.ok ? 'Порядок сохранён' : 'Не удалось сохранить порядок. Обновите страницу.';
        }).catch(function () {
          status.textContent = 'Не удалось сохранить порядок. Проверьте интернет.';
        });
      }
    });
  }
})();
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/admin/`
Expected: `ok`

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/admin internal/assets
git commit -m "feat: add admin paintings list with drag-to-reorder"
```

---

### Task 14: Admin — add a painting (upload, crop, form)

**Files:**
- Create: `internal/admin/form.go`
- Create: `internal/admin/templates/_crop.html`, `internal/admin/templates/painting_form.html`
- Create: `internal/assets/static/vendor/cropper.min.js`, `cropper.min.css` (downloaded)
- Modify: `internal/assets/static/admin.js` (replace whole file)
- Modify: `internal/admin/paintings.go` (add `newForm`, `create`, `formView`, `renderForm`)
- Modify: `internal/admin/admin.go` (`Register`)
- Test: `internal/admin/create_test.go`

**Interfaces:**
- Consumes: `gallery.AddPainting`, `PaintingInput.Validate`, `images.ErrDecode`; Task 12–13 helpers (`addPainting` from Task 13 tests).
- Produces (used by Tasks 15–16):
  - In `form.go`: `parsePaintingForm(r) (gallery.PaintingInput, string /*year text*/, gallery.FieldErrors)`, `parseCrop(r) images.Crop`, `readPhoto(r) ([]byte, error)` (nil, nil when no file), `cropChanged(r) bool`, `merge(dst, src gallery.FieldErrors)`, `photoError(err error) string`, `cropJSON(c images.Crop) string`; messages `msgPhotoTooBig`, `msgPhotoUnreadable`, `msgPhotoMissing`, `msgPhotoAgain`
  - `type cropField struct { CurrentURL, OriginalURL, CropJSON, Error string; Required bool }` rendered by partial `{{template "crop" .}}`
  - `type formView struct { Painting *store.Painting; Input gallery.PaintingInput; YearText string; Errors gallery.FieldErrors; Crop cropField }`; `a.renderForm(w, r, status, formView)`
  - Routes: `GET /admin/paintings/new`, `POST /admin/paintings/new`
  - Form fields: `csrf, title, technique, size, year, description, visible ("on"), photo (file), crop_x, crop_y, crop_w, crop_h, crop_rotate, crop_changed ("1")`
  - Test helper `multipartReq(t, path, fields map[string]string, photo []byte) *http.Request`

- [ ] **Step 1: Vendor Cropper.js**

```bash
curl -fsSLo internal/assets/static/vendor/cropper.min.js https://cdnjs.cloudflare.com/ajax/libs/cropperjs/1.6.2/cropper.min.js
curl -fsSLo internal/assets/static/vendor/cropper.min.css https://cdnjs.cloudflare.com/ajax/libs/cropperjs/1.6.2/cropper.min.css
head -c 80 internal/assets/static/vendor/cropper.min.js
```
Expected: starts with `/*!` and mentions `Cropper.js v1.6.2`.

- [ ] **Step 2: Write the failing tests**

`internal/admin/create_test.go`:
```go
package admin

import (
	"bytes"
	"context"
	"image/color"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func multipartReq(t *testing.T, path string, fields map[string]string, photo []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if photo != nil {
		fw, err := mw.CreateFormFile("photo", "photo.jpg")
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(photo)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestNewFormRequiresLogin(t *testing.T) {
	if rec := newHarness(t).get("/admin/paintings/new", nil); location(rec) != "/admin/login" {
		t.Fatalf("location = %q", location(rec))
	}
}

func TestNewFormRenders(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	body := h.get("/admin/paintings/new", c).Body.String()
	for _, want := range []string{"Новая картина", `name="title"`, "data-crop", "data-required", "Выбрать фото",
		`name="visible" checked`, "/static/vendor/cropper.min.js", "/static/vendor/cropper.min.css", "data-dirty-check"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

func TestCreatePainting(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := multipartReq(t, "/admin/paintings/new", map[string]string{
		"csrf": csrf, "title": "Пион", "technique": "Бумага, акварель", "size": "20×30 см", "year": "2025",
		"description": "Абзац", "visible": "on", "crop_x": "0", "crop_y": "0", "crop_w": "100", "crop_h": "100",
		"crop_rotate": "0", "crop_changed": "1",
	}, testutil.JPEG(t, 300, 200, color.White))
	rec := h.do(req, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings?msg=saved" {
		t.Fatalf("status = %d, location = %q, body = %s", rec.Code, location(rec), rec.Body)
	}
	all, _ := h.st.ListPaintings(context.Background(), false)
	if len(all) != 1 {
		t.Fatalf("paintings = %d", len(all))
	}
	p := all[0]
	if p.Title != "Пион" || p.Year != 2025 || !p.Visible || p.ImageWidth != 100 || p.ImageHeight != 100 {
		t.Fatalf("painting = %+v", p)
	}
}

func TestCreateValidationKeepsInput(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	req := multipartReq(t, "/admin/paintings/new", map[string]string{
		"csrf": csrf, "title": " ", "technique": "Холст, масло", "year": "abc",
	}, testutil.JPEG(t, 30, 20, color.White))
	rec := h.do(req, c)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Укажите название", "Год должен быть числом", "После ошибки фото нужно выбрать ещё раз",
		`value="Холст, масло"`, `value="abc"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if all, _ := h.st.ListPaintings(context.Background(), false); len(all) != 0 {
		t.Fatal("nothing must be stored")
	}
}

func TestCreateRequiresPhoto(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/new", map[string]string{"csrf": csrf, "title": "Пион"}, nil), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Выберите фото") {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCreateRejectsUnreadablePhoto(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/new", map[string]string{"csrf": csrf, "title": "Пион"}, []byte("not an image")), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Не удалось прочитать фото") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestCreateRequiresCSRF(t *testing.T) {
	h := newHarness(t)
	c, _ := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/new", map[string]string{"title": "Пион"}, testutil.JPEG(t, 10, 10, color.White)), c)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCropJSON(t *testing.T) {
	if got := cropJSON(images.Crop{}); got != "" {
		t.Errorf("empty crop: got %q", got)
	}
	got := cropJSON(images.Crop{X: 1, Y: 2, W: 3, H: 4, Rotation: 90})
	if got != `{"height":4,"rotate":90,"width":3,"x":1,"y":2}` {
		t.Errorf("got %q", got)
	}
}
```
(add `"github.com/lilxtent/pictures-gallery/internal/images"` to the imports.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/admin/`
Expected: FAIL — `undefined: cropJSON`

- [ ] **Step 4: Write `form.go`**

`internal/admin/form.go`:
```go
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
```

- [ ] **Step 5: Add the create handlers**

Append to `internal/admin/paintings.go` (add imports `"github.com/lilxtent/pictures-gallery/internal/gallery"` and `"github.com/lilxtent/pictures-gallery/internal/store"`):
```go
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
```

In `internal/admin/admin.go` `Register`, add before the catch-all:
```go
	mux.HandleFunc("GET /admin/paintings/new", a.requireAuth(a.newForm))
	mux.HandleFunc("POST /admin/paintings/new", a.requireAuth(a.create))
```

- [ ] **Step 6: Write the templates**

`internal/admin/templates/_crop.html`:
```html
{{define "crop"}}
<fieldset class="photo" data-crop{{if .Required}} data-required{{end}}>
  <legend>Фото</legend>
  {{with .CurrentURL}}<div class="photo-current"><img src="{{.}}" alt=""></div>{{end}}
  <div class="crop-stage" hidden><img alt=""></div>
  <div class="crop-tools" hidden>
    <button type="button" data-rotate="-90">↺ Повернуть влево</button>
    <button type="button" data-rotate="90">↻ Повернуть вправо</button>
  </div>
  <p class="hint crop-hint" hidden>Передвиньте и растяните рамку так, чтобы в неё попала только картина.</p>
  <div class="actions">
    <label class="button">{{if .CurrentURL}}Заменить фото{{else}}Выбрать фото{{end}}
      <input type="file" name="photo" accept="image/jpeg,image/png,image/webp" hidden>
    </label>
    {{with .OriginalURL}}<button type="button" data-recrop data-original="{{.}}" data-crop="{{$.CropJSON}}">Изменить кадрирование</button>{{end}}
  </div>
  {{with .Error}}<p class="error">{{.}}</p>{{end}}
  <input type="hidden" name="crop_x">
  <input type="hidden" name="crop_y">
  <input type="hidden" name="crop_w">
  <input type="hidden" name="crop_h">
  <input type="hidden" name="crop_rotate">
  <input type="hidden" name="crop_changed">
</fieldset>
{{end}}
```

`internal/admin/templates/painting_form.html`:
```html
{{define "head"}}<link rel="stylesheet" href="/static/vendor/cropper.min.css">{{end}}

{{define "content"}}
{{$p := .Data.Painting}}
<p><a href="/admin/paintings">← Все картины</a></p>
<h1>{{if $p}}{{$p.Title}}{{else}}Новая картина{{end}}</h1>
<form method="post" enctype="multipart/form-data" class="form" data-dirty-check
      action="{{if $p}}/admin/paintings/{{$p.ID}}{{else}}/admin/paintings/new{{end}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  {{template "crop" .Data.Crop}}
  <label>Название
    <input type="text" name="title" value="{{.Data.Input.Title}}" required maxlength="200">
    {{with .Data.Errors.title}}<span class="error">{{.}}</span>{{end}}
  </label>
  <label>Техника <small>например: Бумага, акварель</small>
    <input type="text" name="technique" value="{{.Data.Input.Technique}}" maxlength="200">
  </label>
  <label>Размер <small>например: 30×40 см</small>
    <input type="text" name="size" value="{{.Data.Input.Size}}" maxlength="100">
  </label>
  <label>Год <small>можно оставить пустым</small>
    <input type="text" name="year" value="{{.Data.YearText}}" inputmode="numeric" maxlength="4">
    {{with .Data.Errors.year}}<span class="error">{{.}}</span>{{end}}
  </label>
  <label>Описание <small>пустая строка начинает новый абзац</small>
    <textarea name="description">{{.Data.Input.Description}}</textarea>
  </label>
  <label class="check"><input type="checkbox" name="visible"{{if .Data.Input.Visible}} checked{{end}}> Показывать на сайте</label>
  <div class="actions">
    <button class="primary">Сохранить</button>
    {{if $p}}{{if $p.Visible}}<a href="/paintings/{{$p.Slug}}" target="_blank" rel="noopener">Посмотреть на сайте ↗</a>{{end}}{{end}}
  </div>
</form>
{{if $p}}
<div class="danger-zone">
  <a class="button danger" href="/admin/paintings/{{$p.ID}}/delete">Удалить картину</a>
</div>
{{end}}
{{end}}

{{define "scripts"}}
<script src="/static/vendor/cropper.min.js"></script>
<script src="/static/admin.js"></script>
{{end}}
```

Note: the year field is `type="text" inputmode="numeric"` (not `type="number"`) so an invalid value like "abc" reaches the server and comes back with a clear Russian message instead of a browser-specific bubble.

- [ ] **Step 7: Replace `admin.js` with the full version**

`internal/assets/static/admin.js`:
```js
// Admin helpers: photo cropping, unsaved-changes warning, drag-to-reorder.
(function () {
  'use strict';
  var MAX_BYTES = 30 * 1024 * 1024;

  // Photo cropping with Cropper.js. The browser only measures the crop; the
  // server cuts the stored original, so nothing is lost by cropping.
  document.querySelectorAll('[data-crop]').forEach(function (box) {
    var form = box.closest('form');
    var input = box.querySelector('input[type=file]');
    var stage = box.querySelector('.crop-stage');
    var img = stage.querySelector('img');
    var tools = box.querySelector('.crop-tools');
    var hint = box.querySelector('.crop-hint');
    var current = box.querySelector('.photo-current');
    var recrop = box.querySelector('[data-recrop]');
    var cropper = null;

    function field(name) { return form.querySelector('input[name="' + name + '"]'); }

    function start(src, initial) {
      if (cropper) { cropper.destroy(); cropper = null; }
      stage.hidden = false;
      tools.hidden = false;
      hint.hidden = false;
      if (current) current.hidden = true;
      img.onload = function () {
        img.onload = null;
        cropper = new Cropper(img, {
          viewMode: 1,
          autoCropArea: 1,
          checkOrientation: false, // the browser already applies EXIF orientation, like the server
          background: false,
          zoomable: false,
          data: initial || undefined
        });
      };
      img.src = src;
      field('crop_changed').value = '1';
      form.dispatchEvent(new Event('change'));
    }

    input.addEventListener('change', function () {
      var file = input.files && input.files[0];
      if (!file) return;
      if (file.size > MAX_BYTES) {
        alert('Фото слишком большое (максимум 30 МБ)');
        input.value = '';
        return;
      }
      start(URL.createObjectURL(file));
    });

    if (recrop) {
      recrop.addEventListener('click', function () {
        var initial = recrop.getAttribute('data-crop');
        start(recrop.getAttribute('data-original'), initial ? JSON.parse(initial) : null);
      });
    }

    tools.querySelectorAll('[data-rotate]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        if (cropper) cropper.rotate(Number(btn.getAttribute('data-rotate')));
      });
    });

    form.addEventListener('submit', function (e) {
      if (box.hasAttribute('data-required') && !(input.files && input.files.length)) {
        e.preventDefault();
        alert('Выберите фото картины');
        return;
      }
      if (!cropper) return;
      var d = cropper.getData(true);
      field('crop_x').value = d.x;
      field('crop_y').value = d.y;
      field('crop_w').value = d.width;
      field('crop_h').value = d.height;
      field('crop_rotate').value = d.rotate || 0;
    });
  });

  // Warn before leaving a form with unsaved changes.
  document.querySelectorAll('form[data-dirty-check]').forEach(function (form) {
    var dirty = false;
    form.addEventListener('input', function () { dirty = true; });
    form.addEventListener('change', function () { dirty = true; });
    form.addEventListener('submit', function (e) { if (!e.defaultPrevented) dirty = false; });
    window.addEventListener('beforeunload', function (e) {
      if (dirty) { e.preventDefault(); e.returnValue = ''; }
    });
  });

  // Drag-to-reorder on the paintings list.
  var list = document.querySelector('[data-sortable]');
  if (list && window.Sortable) {
    var status = document.querySelector('[data-sort-status]');
    Sortable.create(list, {
      handle: '.drag',
      animation: 150,
      onEnd: function () {
        var ids = Array.prototype.map.call(list.querySelectorAll('[data-id]'), function (li) {
          return Number(li.getAttribute('data-id'));
        });
        status.textContent = 'Сохраняю…';
        fetch('/admin/paintings/reorder', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': list.getAttribute('data-csrf') },
          body: JSON.stringify({ ids: ids })
        }).then(function (r) {
          status.textContent = r.ok ? 'Порядок сохранён' : 'Не удалось сохранить порядок. Обновите страницу.';
        }).catch(function () {
          status.textContent = 'Не удалось сохранить порядок. Проверьте интернет.';
        });
      }
    });
  }
})();
```

The dirty-check `submit` listener is registered after the crop listener (both on the same form), so `e.defaultPrevented` already reflects the «Выберите фото» check.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/admin/`
Expected: `ok`

- [ ] **Step 9: Manual check of cropping**

Start `DEV=1 ADMIN_PASSWORD=dev-password go run ./cmd/gallery`, log in at `http://localhost:8080/admin`, click «+ Добавить картину», choose `internal/seed/photos/peony.jpg`, drag the frame to exclude the pen and stamp, press «↻ Повернуть вправо» once and back, fill «Название», save. On the public site the new painting's image must match the frame exactly. Repeat once **with** a 90° rotation and confirm the saved image is rotated clockwise and cropped to the frame you saw. Test at the mobile preset too (the frame must be draggable by touch-sized handles). Stop the server.

- [ ] **Step 10: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/admin internal/assets
git commit -m "feat: add painting upload with in-browser cropping"
```

---

### Task 15: Admin — edit, re-crop, replace photo, delete

**Files:**
- Modify: `internal/admin/paintings.go` (add edit/update/original/delete handlers)
- Create: `internal/admin/templates/delete.html`
- Modify: `internal/admin/admin.go` (`Register`)
- Test: `internal/admin/edit_test.go`

**Interfaces:**
- Consumes: Task 14 (`formView`, `renderForm`, `cropField`, form helpers, `multipartReq`), Task 13 (`addPainting` test helper), `gallery.UpdatePainting/DeletePainting`, `images.Disk.LoadOriginal`.
- Produces:
  - Routes: `GET /admin/paintings/{id}`, `POST /admin/paintings/{id}`, `GET /admin/paintings/{id}/original`, `GET /admin/paintings/{id}/delete`, `POST /admin/paintings/{id}/delete`
  - `a.serveOriginal(w, r, dir string)` (reused by Task 16 for the author photo)

- [ ] **Step 1: Write the failing tests**

`internal/admin/edit_test.go`:
```go
package admin

import (
	"context"
	"errors"
	"image/color"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestEditFormShowsValues(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p, _ := h.g.AddPainting(ctx, gallery.PaintingInput{Title: "Пион", Technique: "Бумага, акварель", Year: 2025, Visible: true},
		gallery.Photo{Original: testutil.JPEG(t, 300, 200, color.White), Crop: images.Crop{W: 100, H: 80}})
	c, _ := h.login()
	body := h.get("/admin/paintings/1", c).Body.String()
	for _, want := range []string{`value="Пион"`, `value="Бумага, акварель"`, `value="2025"`, "Заменить фото",
		"Изменить кадрирование", `data-original="/admin/paintings/1/original"`, "width", "Удалить картину",
		`href="/paintings/` + p.Slug + `"`, images.URL(images.PaintingDir(p.ID), 1, 600)} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
	if strings.Contains(body, "data-required") {
		t.Error("editing must not require a new photo")
	}
}

func TestEditUnknownOrBadID(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	for _, path := range []string{"/admin/paintings/99", "/admin/paintings/abc"} {
		if rec := h.get(path, c); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d", path, rec.Code)
		}
	}
	if rec := h.postForm("/admin/paintings/99", url.Values{"csrf": {csrf}, "title": {"x"}}, c); rec.Code != http.StatusNotFound {
		t.Errorf("POST unknown: status = %d", rec.Code)
	}
}

func TestUpdateTextOnly(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Розовый пион", "year": "2024"}, nil), c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings?msg=saved" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	p, _ := h.st.GetPainting(context.Background(), 1)
	if p.Title != "Розовый пион" || p.Year != 2024 || p.Visible || p.ImageVersion != 1 {
		t.Fatalf("painting = %+v", p)
	}
}

func TestUpdateRecrop(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{
		"csrf": csrf, "title": "Пион", "visible": "on",
		"crop_changed": "1", "crop_x": "10", "crop_y": "10", "crop_w": "50", "crop_h": "40", "crop_rotate": "0",
	}, nil), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	p, _ := h.st.GetPainting(context.Background(), 1)
	if p.ImageVersion != 2 || p.ImageWidth != 50 || p.ImageHeight != 40 {
		t.Fatalf("painting = %+v", p)
	}
}

func TestUpdateReplacesPhoto(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": "Пион"},
		testutil.PNG(t, 40, 30, color.Black)), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(h.g.Disk.Root, "paintings", "1", "original.png")); err != nil {
		t.Fatal("new original should be stored")
	}
}

func TestUpdateValidation(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/paintings/1", map[string]string{"csrf": csrf, "title": ""}, nil), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Укажите название") {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<h1>Пион</h1>") {
		t.Error("the page heading should keep the saved title")
	}
}

func TestOriginalIsPrivate(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	if rec := h.get("/admin/paintings/1/original", nil); location(rec) != "/admin/login" {
		t.Fatalf("logged out: location = %q", location(rec))
	}
	c, _ := h.login()
	rec := h.get("/admin/paintings/1/original", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d, headers = %v", rec.Code, rec.Header())
	}
}

func TestDeleteFlow(t *testing.T) {
	h := newHarness(t)
	h.addPainting("Пион", true)
	c, csrf := h.login()

	body := h.get("/admin/paintings/1/delete", c).Body.String()
	if !strings.Contains(body, "Удалить картину?") || !strings.Contains(body, "«Пион»") {
		t.Fatalf("confirm page = %s", body)
	}
	if rec := h.postForm("/admin/paintings/1/delete", url.Values{}, c); rec.Code != http.StatusForbidden {
		t.Fatalf("without csrf: status = %d", rec.Code)
	}
	rec := h.postForm("/admin/paintings/1/delete", url.Values{"csrf": {csrf}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/paintings?msg=deleted" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	if _, err := h.st.GetPainting(context.Background(), 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("painting should be deleted")
	}
	if body := h.get("/admin/paintings?msg=deleted", c).Body.String(); !strings.Contains(body, "Картина удалена") {
		t.Error("flash message missing")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/admin/ -run 'Edit|Update|Original|Delete'`
Expected: FAIL (routes missing → redirects/404)

- [ ] **Step 3: Write the handlers**

Append to `internal/admin/paintings.go` (add imports `"errors"`, `"os"`, `"strconv"`, `"github.com/lilxtent/pictures-gallery/internal/images"`):
```go
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
```

In `internal/admin/admin.go` `Register`, add before the catch-all:
```go
	mux.HandleFunc("GET /admin/paintings/{id}", a.requireAuth(a.editForm))
	mux.HandleFunc("POST /admin/paintings/{id}", a.requireAuth(a.update))
	mux.HandleFunc("GET /admin/paintings/{id}/original", a.requireAuth(a.paintingOriginal))
	mux.HandleFunc("GET /admin/paintings/{id}/delete", a.requireAuth(a.deleteConfirm))
	mux.HandleFunc("POST /admin/paintings/{id}/delete", a.requireAuth(a.deletePainting))
```

- [ ] **Step 4: Write the delete template**

`internal/admin/templates/delete.html`:
```html
{{define "content"}}
<h1>Удалить картину?</h1>
<p>«{{.Data.Title}}» будет удалена с сайта вместе с фото. Это нельзя отменить.</p>
<form method="post" action="/admin/paintings/{{.Data.ID}}/delete" class="actions">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <button class="danger">Да, удалить</button>
  <a class="button" href="/admin/paintings/{{.Data.ID}}">Отмена</a>
</form>
{{end}}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/admin/`
Expected: `ok`

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/admin
git commit -m "feat: add painting editing, re-cropping and deletion"
```

---

### Task 16: Admin — «Об авторе», «Настройки», password change

**Files:**
- Create: `internal/admin/pages.go`
- Create: `internal/admin/templates/about.html`, `internal/admin/templates/settings.html`
- Modify: `internal/admin/admin.go` (`Register`)
- Test: `internal/admin/pages_test.go`

**Interfaces:**
- Consumes: Task 14 form helpers and `cropField`, Task 15 `serveOriginal`, `gallery.SetAboutPhoto/CleanText`, `site` keys, `checkPassword/setPassword` (Task 12).
- Produces: routes `GET|POST /admin/about`, `GET /admin/about/original`, `GET|POST /admin/settings`, `POST /admin/password`.

- [ ] **Step 1: Write the failing tests**

`internal/admin/pages_test.go`:
```go
package admin

import (
	"context"
	"image/color"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lilxtent/pictures-gallery/internal/site"
	"github.com/lilxtent/pictures-gallery/internal/testutil"
)

func TestAboutSaveText(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/about", map[string]string{"csrf": csrf, "about_text": "Строка\r\n\r\nВторая "}, nil), c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/about?msg=saved" {
		t.Fatalf("status = %d, location = %q", rec.Code, location(rec))
	}
	info, _ := site.Load(context.Background(), h.st)
	if info.AboutText != "Строка\n\nВторая" {
		t.Fatalf("AboutText = %q", info.AboutText)
	}
	body := h.get("/admin/about?msg=saved", c).Body.String()
	if !strings.Contains(body, "Сохранено") || !strings.Contains(body, "Строка") {
		t.Error("form should show the saved text and the flash")
	}
}

func TestAboutPhotoUploadAndRecrop(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/about", map[string]string{"csrf": csrf, "about_text": "Обо мне"},
		testutil.JPEG(t, 300, 400, color.White)), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status = %d, body = %s", rec.Code, rec.Body)
	}
	body := h.get("/admin/about", c).Body.String()
	if !strings.Contains(body, "Изменить кадрирование") || !strings.Contains(body, `data-original="/admin/about/original"`) {
		t.Error("re-crop button missing after upload")
	}
	rec = h.do(multipartReq(t, "/admin/about", map[string]string{
		"csrf": csrf, "about_text": "Обо мне", "crop_changed": "1", "crop_w": "100", "crop_h": "100",
	}, nil), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("recrop: status = %d", rec.Code)
	}
	info, _ := site.Load(context.Background(), h.st)
	if info.AboutPhoto == nil || info.AboutPhoto.Version != 2 || info.AboutPhoto.Width != 100 {
		t.Fatalf("AboutPhoto = %+v", info.AboutPhoto)
	}
	if rec := h.get("/admin/about/original", c); rec.Code != http.StatusOK {
		t.Errorf("original: status = %d", rec.Code)
	}
}

func TestAboutBadPhoto(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.do(multipartReq(t, "/admin/about", map[string]string{"csrf": csrf, "about_text": "Текст"}, []byte("nope")), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Не удалось прочитать фото") {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Текст") {
		t.Error("typed text must be kept")
	}
}

func TestSettingsSave(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	form := url.Values{
		"csrf": {csrf}, site.KeyArtistName: {" Анна Иванова "}, site.KeySubtitle: {"художник, акварель"},
		site.KeyGreeting: {"Здравствуйте!\r\n"}, site.KeyPhone: {"+7 900 000-00-00"}, site.KeyEmail: {"anna@example.ru"},
		site.KeyTelegram: {"@anna"}, site.KeyWhatsApp: {"+7 900 000-00-00"}, site.KeyVK: {"anna"},
	}
	rec := h.postForm("/admin/settings", form, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/settings?msg=saved" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	info, _ := site.Load(context.Background(), h.st)
	if info.ArtistName != "Анна Иванова" || info.Greeting != "Здравствуйте!" || info.Telegram != "@anna" {
		t.Fatalf("info = %+v", info)
	}
	if body := h.get("/admin/settings", c).Body.String(); !strings.Contains(body, `value="Анна Иванова"`) {
		t.Error("form should show saved values")
	}
}

func TestSettingsInvalidEmail(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.postForm("/admin/settings", url.Values{"csrf": {csrf}, site.KeyEmail: {"not-an-email"}, site.KeyArtistName: {"Анна"}}, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Проверьте адрес почты") {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `value="Анна"`) {
		t.Error("typed values must be kept")
	}
}

func TestChangePassword(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	rec := h.postForm("/admin/password", url.Values{"csrf": {csrf}, "current": {testPassword}, "new": {"new password 1"}, "repeat": {"new password 1"}}, c)
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/settings?msg=password" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	ctx := context.Background()
	if ok, _ := checkPassword(ctx, h.st, "new password 1"); !ok {
		t.Error("new password must work")
	}
	if ok, _ := checkPassword(ctx, h.st, testPassword); ok {
		t.Error("old password must stop working")
	}
}

func TestChangePasswordErrors(t *testing.T) {
	h := newHarness(t)
	c, csrf := h.login()
	cases := []struct {
		current, newPw, repeat, want string
	}{
		{"wrong", "new password 1", "new password 1", "Неверный текущий пароль"},
		{testPassword, "short", "short", "не короче 8 символов"},
		{testPassword, "new password 1", "new password 2", "Пароли не совпадают"},
	}
	for _, tc := range cases {
		rec := h.postForm("/admin/password", url.Values{"csrf": {csrf}, "current": {tc.current}, "new": {tc.newPw}, "repeat": {tc.repeat}}, c)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%q: status = %d, want message %q", tc.want, rec.Code, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/admin/ -run 'About|Settings|Password'`
Expected: FAIL (routes missing)

- [ ] **Step 3: Write the handlers**

`internal/admin/pages.go`:
```go
package admin

import (
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

type aboutView struct {
	Text   string
	Crop   cropField
	Errors gallery.FieldErrors
}

func aboutCrop(info site.Info, errMsg string) cropField {
	cf := cropField{Error: errMsg}
	if p := info.AboutPhoto; p != nil {
		cf.CurrentURL = images.URL(images.AboutDir, p.Version, 600)
		cf.OriginalURL = "/admin/about/original"
		cf.CropJSON = cropJSON(p.Crop)
	}
	return cf
}

func (a *Admin) aboutForm(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), a.st)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "about.html", view{Title: "Об авторе", Nav: "about",
		Data: aboutView{Text: info.AboutText, Crop: aboutCrop(info, "")}})
}

func (a *Admin) saveAbout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	text := gallery.CleanText(r.PostFormValue("about_text"))
	errs := gallery.FieldErrors{}
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
		if photo != nil {
			if err := a.g.SetAboutPhoto(ctx, *photo); err != nil {
				msg := photoError(err)
				if msg == "" {
					a.serverError(w, r, err)
					return
				}
				errs["photo"] = msg
			}
		}
	}
	if len(errs) == 0 {
		if err := a.st.SetSettings(ctx, map[string]string{site.KeyAboutText: text}); err != nil {
			a.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/about?msg=saved", http.StatusSeeOther)
		return
	}
	info, err := site.Load(ctx, a.st)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusUnprocessableEntity, "about.html", view{Title: "Об авторе", Nav: "about",
		Data: aboutView{Text: text, Crop: aboutCrop(info, errs["photo"]), Errors: errs}})
}

func (a *Admin) aboutOriginal(w http.ResponseWriter, r *http.Request) {
	a.serveOriginal(w, r, images.AboutDir)
}

var settingsFields = []string{
	site.KeyArtistName, site.KeySubtitle, site.KeyGreeting,
	site.KeyPhone, site.KeyEmail, site.KeyTelegram, site.KeyWhatsApp, site.KeyVK,
}

type settingsView struct {
	Values         map[string]string
	Errors         gallery.FieldErrors
	PasswordErrors gallery.FieldErrors
}

func (a *Admin) renderSettings(w http.ResponseWriter, r *http.Request, status int, sv settingsView) {
	a.render(w, r, status, "settings.html", view{Title: "Настройки", Nav: "settings", Data: sv})
}

func (a *Admin) settingsForm(w http.ResponseWriter, r *http.Request) {
	values, err := a.st.Settings(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderSettings(w, r, http.StatusOK, settingsView{Values: values})
}

func (a *Admin) saveSettings(w http.ResponseWriter, r *http.Request) {
	values := map[string]string{}
	for _, k := range settingsFields {
		values[k] = strings.TrimSpace(r.PostFormValue(k))
	}
	values[site.KeyGreeting] = gallery.CleanText(r.PostFormValue(site.KeyGreeting))
	errs := gallery.FieldErrors{}
	if e := values[site.KeyEmail]; e != "" {
		if addr, err := mail.ParseAddress(e); err != nil || addr.Address != e {
			errs[site.KeyEmail] = "Проверьте адрес почты"
		}
	}
	if len(errs) > 0 {
		a.renderSettings(w, r, http.StatusUnprocessableEntity, settingsView{Values: values, Errors: errs})
		return
	}
	if err := a.st.SetSettings(r.Context(), values); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/settings?msg=saved", http.StatusSeeOther)
}

func (a *Admin) changePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ok, err := checkPassword(ctx, a.st, r.PostFormValue("current"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	newPw, repeat := r.PostFormValue("new"), r.PostFormValue("repeat")
	errs := gallery.FieldErrors{}
	switch {
	case !ok:
		errs["current"] = "Неверный текущий пароль"
	case utf8.RuneCountInString(newPw) < 8:
		errs["new"] = "Новый пароль должен быть не короче 8 символов"
	case newPw != repeat:
		errs["repeat"] = "Пароли не совпадают"
	}
	if len(errs) > 0 {
		values, err := a.st.Settings(ctx)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		a.renderSettings(w, r, http.StatusUnprocessableEntity, settingsView{Values: values, PasswordErrors: errs})
		return
	}
	if err := setPassword(ctx, a.st, newPw); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/settings?msg=password", http.StatusSeeOther)
}
```

In `internal/admin/admin.go` `Register`, add before the catch-all:
```go
	mux.HandleFunc("GET /admin/about", a.requireAuth(a.aboutForm))
	mux.HandleFunc("POST /admin/about", a.requireAuth(a.saveAbout))
	mux.HandleFunc("GET /admin/about/original", a.requireAuth(a.aboutOriginal))
	mux.HandleFunc("GET /admin/settings", a.requireAuth(a.settingsForm))
	mux.HandleFunc("POST /admin/settings", a.requireAuth(a.saveSettings))
	mux.HandleFunc("POST /admin/password", a.requireAuth(a.changePassword))
```

- [ ] **Step 4: Write the templates**

`internal/admin/templates/about.html`:
```html
{{define "head"}}<link rel="stylesheet" href="/static/vendor/cropper.min.css">{{end}}

{{define "content"}}
<h1>Об авторе</h1>
<form method="post" action="/admin/about" enctype="multipart/form-data" class="form" data-dirty-check>
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  {{template "crop" .Data.Crop}}
  <label>Рассказ об авторе <small>пустая строка начинает новый абзац</small>
    <textarea name="about_text">{{.Data.Text}}</textarea>
  </label>
  <div class="actions">
    <button class="primary">Сохранить</button>
    <a href="/about" target="_blank" rel="noopener">Посмотреть на сайте ↗</a>
  </div>
</form>
{{end}}

{{define "scripts"}}
<script src="/static/vendor/cropper.min.js"></script>
<script src="/static/admin.js"></script>
{{end}}
```

`internal/admin/templates/settings.html`:
```html
{{define "content"}}
<h1>Настройки</h1>
<form method="post" action="/admin/settings" class="form" data-dirty-check>
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset>
    <legend>Шапка сайта</legend>
    <label>Имя <small>как показывать на сайте</small>
      <input type="text" name="artist_name" value="{{index .Data.Values "artist_name"}}" maxlength="100">
    </label>
    <label>Подпись под именем <small>например: художник, акварель</small>
      <input type="text" name="subtitle" value="{{index .Data.Values "subtitle"}}" maxlength="100">
    </label>
    <label>Приветствие на главной странице <small>если оставить пустым, блок не показывается</small>
      <textarea class="short" name="greeting">{{index .Data.Values "greeting"}}</textarea>
    </label>
  </fieldset>
  <fieldset>
    <legend>Контакты <small>пустые поля на сайте не показываются</small></legend>
    <label>Телефон
      <input type="tel" name="phone" value="{{index .Data.Values "phone"}}" maxlength="50">
    </label>
    <label>Почта
      <input type="email" name="email" value="{{index .Data.Values "email"}}" maxlength="100">
      {{with .Data.Errors.email}}<span class="error">{{.}}</span>{{end}}
    </label>
    <label>Telegram <small>имя пользователя, например @anna_art</small>
      <input type="text" name="telegram" value="{{index .Data.Values "telegram"}}" maxlength="100">
    </label>
    <label>WhatsApp <small>номер телефона</small>
      <input type="tel" name="whatsapp" value="{{index .Data.Values "whatsapp"}}" maxlength="50">
    </label>
    <label>ВКонтакте <small>ссылка на страницу или её адрес</small>
      <input type="text" name="vk" value="{{index .Data.Values "vk"}}" maxlength="200">
    </label>
  </fieldset>
  <div class="actions"><button class="primary">Сохранить</button></div>
</form>

<form method="post" action="/admin/password" class="form danger-zone">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset>
    <legend>Сменить пароль</legend>
    <label>Текущий пароль
      <input type="password" name="current" autocomplete="current-password" required>
      {{with .Data.PasswordErrors.current}}<span class="error">{{.}}</span>{{end}}
    </label>
    <label>Новый пароль <small>не короче 8 символов</small>
      <input type="password" name="new" autocomplete="new-password" required minlength="8">
      {{with .Data.PasswordErrors.new}}<span class="error">{{.}}</span>{{end}}
    </label>
    <label>Новый пароль ещё раз
      <input type="password" name="repeat" autocomplete="new-password" required minlength="8">
      {{with .Data.PasswordErrors.repeat}}<span class="error">{{.}}</span>{{end}}
    </label>
  </fieldset>
  <div class="actions"><button>Сменить пароль</button></div>
</form>
{{end}}

{{define "scripts"}}<script src="/static/admin.js"></script>{{end}}
```

`index` on a nil map returns the zero value, so the template works before any settings exist.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./...`
Expected: all `ok`

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/admin
git commit -m "feat: add author page, site settings and password change to admin"
```

---

### Task 17: Deployment — Docker, Caddy, backups, README

**Files:**
- Create: `deploy/Dockerfile`, `deploy/docker-compose.yml`, `deploy/Caddyfile`, `deploy/.env.example`
- Create: `deploy/backup.sh`, `deploy/deploy.sh` (executable)
- Create: `.dockerignore`
- Modify: `.gitignore` (add `deploy/.env`)
- Create: `README.md`

**Interfaces:**
- Consumes: the `gallery` binary and its env vars (Task 11), `gallery backup <file>` subcommand.
- Produces: `docker compose -f deploy/docker-compose.yml up -d --build` runs the site with HTTPS for `$DOMAIN`.

- [ ] **Step 1: Write the Dockerfile and ignore files**

`deploy/Dockerfile`:
```dockerfile
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gallery ./cmd/gallery

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 gallery \
 && mkdir /data && chown gallery /data
COPY --from=build /out/gallery /usr/local/bin/gallery
USER gallery
ENV DATA_DIR=/data ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["gallery"]
```

`.dockerignore`:
```
.git
.superpowers
data
deploy/.env
```

Append to `.gitignore`:
```
deploy/.env
```

- [ ] **Step 2: Write Compose, Caddy and env example**

`deploy/docker-compose.yml`:
```yaml
services:
  app:
    build:
      context: ..
      dockerfile: deploy/Dockerfile
    restart: unless-stopped
    env_file: .env
    environment:
      DATA_DIR: /data
      TRUST_PROXY: "1"
    volumes:
      - ../data:/data

  caddy:
    image: caddy:2-alpine
    restart: unless-stopped
    depends_on: [app]
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    environment:
      DOMAIN: ${DOMAIN}
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config

volumes:
  caddy_data:
  caddy_config:
```

`deploy/Caddyfile`:
```
{$DOMAIN} {
	encode zstd gzip
	request_body {
		max_size 32MB
	}
	reverse_proxy app:8080
}

www.{$DOMAIN} {
	redir https://{$DOMAIN}{uri} permanent
}
```

`deploy/.env.example`:
```bash
# Domain the site is served on; Caddy obtains the HTTPS certificate for it.
DOMAIN=example.ru
# Absolute site address, used in link previews and the sitemap.
BASE_URL=https://example.ru
# Admin password, used only on the very first start. Change it later in «Настройки».
ADMIN_PASSWORD=change-me-please
# rclone destination for nightly backups (remote:bucket/path), see README.
BACKUP_TARGET=backup:gallery-backups
```

- [ ] **Step 3: Write the scripts**

`deploy/backup.sh`:
```bash
#!/usr/bin/env bash
# Nightly backup: database snapshot + images to S3-compatible storage via rclone.
# Cron (root): 30 3 * * * /srv/gallery/deploy/backup.sh >> /var/log/gallery-backup.log 2>&1
set -euo pipefail
cd "$(dirname "$0")"
set -a
. ./.env
set +a
: "${BACKUP_TARGET:?set BACKUP_TARGET in deploy/.env}"
stamp=$(date +%F)

docker compose exec -T app gallery backup /data/backup/gallery.db
rclone copyto ../data/backup/gallery.db "$BACKUP_TARGET/db/gallery-$stamp.db"
rclone sync ../data/images "$BACKUP_TARGET/images" --backup-dir "$BACKUP_TARGET/images-old/$stamp"
rclone delete --min-age 30d "$BACKUP_TARGET/db"
rclone delete --min-age 30d "$BACKUP_TARGET/images-old"
rclone rmdirs --leave-root "$BACKUP_TARGET/images-old"
echo "$(date -Is) backup $stamp done"
```

`deploy/deploy.sh`:
```bash
#!/usr/bin/env bash
# Update the server to the latest main:
#   DEPLOY_HOST=root@203.0.113.10 deploy/deploy.sh
set -euo pipefail
: "${DEPLOY_HOST:?set DEPLOY_HOST, e.g. root@203.0.113.10}"
DEPLOY_DIR=${DEPLOY_DIR:-/srv/gallery}
ssh "$DEPLOY_HOST" "cd '$DEPLOY_DIR' && git pull --ff-only && docker compose -f deploy/docker-compose.yml up -d --build && docker image prune -f"
```

```bash
chmod +x deploy/backup.sh deploy/deploy.sh
bash -n deploy/backup.sh && bash -n deploy/deploy.sh
```
Expected: no output (syntax OK).

- [ ] **Step 4: Write the README**

`README.md`:
````markdown
# Pictures gallery

Portfolio website for a watercolour painter: a public gallery in Russian and an
admin panel at `/admin` where she adds, crops, describes and orders her
paintings herself. Design: `docs/superpowers/specs/2026-10-04-painter-portfolio-design.md`.

## Local development

Requires Go 1.24+.

```bash
DEV=1 ADMIN_PASSWORD=dev-password go run ./cmd/gallery
```

- Site: http://localhost:8080 (an empty database is filled with three sample paintings)
- Admin: http://localhost:8080/admin, password `dev-password` (local development only)
- Data lives in `./data` (gitignored). Delete it to start over.
- Tests: `go test ./...`

Environment variables: `ADDR` (`:8080`), `DATA_DIR` (`./data`), `BASE_URL`
(`http://localhost:8080`), `ADMIN_PASSWORD` (first start only), `DEV=1`
(no Secure cookie, sample content), `TRUST_PROXY=1` (client IP from `X-Forwarded-For`).

## Production server (one-time setup)

1. Rent a small VPS with Ubuntu 24.04 at a Russian provider (e.g. Timeweb Cloud) and
   register a `.ru` domain. Point the `@` and `www` A records to the server's IP.
2. On the server:
   ```bash
   apt update && apt install -y docker.io docker-compose-v2 git rclone
   git clone https://github.com/lilxtent/pictures-gallery.git /srv/gallery
   cd /srv/gallery
   cp deploy/.env.example deploy/.env   # then edit DOMAIN, BASE_URL, ADMIN_PASSWORD, BACKUP_TARGET
   mkdir -p data && chown 10001:10001 data
   docker compose -f deploy/docker-compose.yml up -d --build
   ```
   If image pulls from Docker Hub fail, configure your provider's Docker Hub mirror
   in `/etc/docker/daemon.json` (`"registry-mirrors"`) and restart Docker.
3. Open `https://<domain>/admin`, log in with `ADMIN_PASSWORD`, and have your mother
   set her own password in «Настройки».

## Backups

1. Create an S3-compatible bucket at the same provider and run `rclone config`
   to add a remote named `backup` (type `s3`, provider `Other`, with the provider's
   endpoint and keys).
2. Set `BACKUP_TARGET=backup:<bucket>/gallery` in `deploy/.env`.
3. Test: `/srv/gallery/deploy/backup.sh`
4. Add to root's crontab (`crontab -e`):
   ```
   30 3 * * * /srv/gallery/deploy/backup.sh >> /var/log/gallery-backup.log 2>&1
   ```

Daily database snapshots are kept for 30 days; images are mirrored, and files
deleted or replaced on the site are kept in `images-old/<date>` for 30 days.

**Restore:** stop the app, copy a `gallery-<date>.db` to `data/gallery.db`, sync
`images` back to `data/images`, `chown -R 10001:10001 data`, start the app.

## Updating

```bash
DEPLOY_HOST=root@<server-ip> deploy/deploy.sh
```
````

- [ ] **Step 5: Verify the image builds and runs**

```bash
docker build -f deploy/Dockerfile -t gallery:test .
docker run --rm -d --name gallery-test -p 8081:8080 -e DEV=1 -e ADMIN_PASSWORD=dev-password gallery:test
sleep 3
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/
curl -s http://localhost:8081/ | grep -c 'Карпы кои'
docker stop gallery-test
cp deploy/.env.example deploy/.env && docker compose -f deploy/docker-compose.yml config >/dev/null && echo compose-ok && rm deploy/.env
```
Expected: `200`, a count ≥ 1, `compose-ok`.

- [ ] **Step 6: Commit**

```bash
git add deploy .dockerignore .gitignore README.md
git commit -m "chore: add Docker/Caddy deployment, backups and README"
```

---

### Task 18: End-to-end verification

**Files:** none expected; fix and commit anything found.

- [ ] **Step 1: Full automated check**

```bash
gofmt -l . ; go vet ./... && go test -count=1 ./...
```
Expected: no `gofmt` output, every package `ok`.

- [ ] **Step 2: Fresh local run**

```bash
rm -rf data
DEV=1 ADMIN_PASSWORD=dev-password go run ./cmd/gallery
```

- [ ] **Step 3: Walk through the painter's tasks in the browser pane, at the mobile preset (375×812)**

Log in at `http://localhost:8080/admin` with `dev-password`, then:
1. «+ Добавить картину» → choose `internal/seed/photos/lily.jpg`, crop off the right edge, rotate right and back, title «Тестовая лилия», technique, size, year, a two-paragraph description → «Сохранить». It appears first in the list with the flash «Сохранено».
2. Open it, change the description, uncheck «Показывать на сайте», save → badge «Скрыта»; the public home page no longer shows it and `/paintings/testovaya-liliya` is a 404.
3. Re-enable it; use «Изменить кадрирование» → the frame starts where it was left; tighten it and save → the public image changes.
4. Drag it to the bottom of the list → «Порядок сохранён»; reload → order persists; public home order matches.
5. «Об авторе»: upload a photo, crop to a portrait, write text → `/about` shows both; the home greeting block shows the round photo.
6. «Настройки»: set name, subtitle, phone, Telegram, WhatsApp, VK, email → header and footer update; `/contacts` links open the right apps/URLs (`tel:`, `https://t.me/…`, `https://wa.me/7…`, `https://vk.com/…`).
7. Start editing a painting, then tap «Картины» → the browser warns about unsaved changes.
8. Delete «Тестовая лилия» via the confirmation page → gone from list and site.
9. Change the password, log out, log in with the new one.

- [ ] **Step 4: Repeat a quick pass on desktop width**

Home grid has 3 columns, painting page is side by side with a sticky image, lightbox opens and closes (click and Esc), admin list is readable.

- [ ] **Step 5: Check no external requests and no visitor cookies**

In the browser pane's network log for the public pages, every request goes to `localhost:8080`, and the public pages set no cookies.

- [ ] **Step 6: Stop the server and commit any fixes**

```bash
git status --short
git commit -am "fix: issues found in end-to-end check"   # only if something was fixed
```

