package images

import (
	"errors"
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
	newName := filepath.Join(p, "original"+ext)
	if err := os.Rename(tmp, newName); err != nil {
		os.Remove(tmp)
		return err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "original.") && name != "original"+ext {
			os.Remove(filepath.Join(p, name))
		}
	}
	return nil
}

// LoadOriginal returns the stored original or an error wrapping os.ErrNotExist.
func (d *Disk) LoadOriginal(dir string) ([]byte, error) {
	p, err := d.path(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("images: no original in %s: %w", dir, os.ErrNotExist)
		}
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "original.") {
			return os.ReadFile(filepath.Join(p, name))
		}
	}
	return nil, fmt.Errorf("images: no original in %s: %w", dir, os.ErrNotExist)
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
	entries, err := os.ReadDir(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	prefix := fmt.Sprintf("v%d-", version)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".jpg") {
			if err := os.Remove(filepath.Join(p, name)); err != nil {
				return err
			}
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
