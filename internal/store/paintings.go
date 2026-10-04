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
		p                Painting
		year             sql.NullInt64
		created, updated string
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
