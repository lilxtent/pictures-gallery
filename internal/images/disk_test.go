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

func TestRemoveVariantsPreservesOtherVersions(t *testing.T) {
	d := &Disk{Root: t.TempDir()}
	variants2 := map[int][]byte{600: []byte("v2-600"), 1200: []byte("v2-1200")}
	variants3 := map[int][]byte{600: []byte("v3-600"), 1200: []byte("v3-1200")}
	if err := d.WriteVariants("paintings/1", 2, variants2); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteVariants("paintings/1", 3, variants3); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveVariants("paintings/1", 2); err != nil {
		t.Fatal(err)
	}
	p, _ := d.VariantFile("paintings/1", "v2-600.jpg")
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("v2 variants should be removed")
	}
	p, _ = d.VariantFile("paintings/1", "v3-600.jpg")
	if b, _ := os.ReadFile(p); string(b) != "v3-600" {
		t.Fatal("v3 variants should be preserved")
	}
}

func TestOriginalWithGlobMetacharactersInRoot(t *testing.T) {
	tmpDir := t.TempDir()
	rootWithMetachar := filepath.Join(tmpDir, "[test]")
	if err := os.Mkdir(rootWithMetachar, 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Disk{Root: rootWithMetachar}
	jpg := testutil.JPEG(t, 10, 10, color.White)
	if err := d.SaveOriginal("paintings/1", jpg); err != nil {
		t.Fatal(err)
	}
	got, err := d.LoadOriginal("paintings/1")
	if err != nil || !bytes.Equal(got, jpg) {
		t.Fatalf("LoadOriginal with glob metacharacters in Root failed: %v", err)
	}
}
