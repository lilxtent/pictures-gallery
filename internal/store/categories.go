package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lilxtent/pictures-gallery/internal/slug"
)

// Category groups paintings by theme.
type Category struct {
	ID       int64
	Slug     string
	Name     string
	Position int // lower comes first on the site

	// Count is the number of paintings in the category. It is only filled
	// by ListCategories (all paintings) and ListPublicCategories (visible ones).
	Count int

	CreatedAt time.Time
	UpdatedAt time.Time
}

const categoryCols = `c.id, c.slug, c.name, c.position, c.created_at, c.updated_at`

func scanCategory(row scanner) (Category, error) {
	var (
		c                Category
		created, updated string
	)
	err := row.Scan(&c.ID, &c.Slug, &c.Name, &c.Position, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, err
	}
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, nil
}

// CreateCategory inserts c at the end of the list and fills in ID, Slug,
// Position and timestamps.
func (s *Store) CreateCategory(ctx context.Context, c *Category) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	base := slug.Make(c.Name)
	if base == "" {
		base = "kategoriya"
	}
	sl, err := uniqueSlug(ctx, tx, "categories", base)
	if err != nil {
		return err
	}
	var maxPos sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(position) FROM categories").Scan(&maxPos); err != nil {
		return err
	}
	pos := 0
	if maxPos.Valid {
		pos = int(maxPos.Int64) + 1
	}
	ts := now()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO categories (slug, name, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		sl, c.Name, pos, ts, ts)
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
	c.ID, c.Slug, c.Position = id, sl, pos
	c.CreatedAt, c.UpdatedAt = parseTime(ts), parseTime(ts)
	return nil
}

// RenameCategory changes the name; the slug stays, so existing links keep working.
func (s *Store) RenameCategory(ctx context.Context, id int64, name string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE categories SET name = ?, updated_at = ? WHERE id = ?", name, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetCategory returns the category with the given id.
func (s *Store) GetCategory(ctx context.Context, id int64) (Category, error) {
	return scanCategory(s.db.QueryRowContext(ctx, "SELECT "+categoryCols+" FROM categories c WHERE c.id = ?", id))
}

// GetCategoryBySlug returns the category with the given slug.
func (s *Store) GetCategoryBySlug(ctx context.Context, sl string) (Category, error) {
	return scanCategory(s.db.QueryRowContext(ctx, "SELECT "+categoryCols+" FROM categories c WHERE c.slug = ?", sl))
}

// ListCategories returns every category in site order, with the number of
// paintings (hidden ones included) in each.
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	return s.listCategories(ctx, false)
}

// ListPublicCategories returns the categories that have at least one visible
// painting, in site order, with Count set to the number of visible paintings.
func (s *Store) ListPublicCategories(ctx context.Context) ([]Category, error) {
	return s.listCategories(ctx, true)
}

func (s *Store) listCategories(ctx context.Context, publicOnly bool) ([]Category, error) {
	join, having := "LEFT JOIN paintings p ON p.category_id = c.id", ""
	if publicOnly {
		join = "LEFT JOIN paintings p ON p.category_id = c.id AND p.visible = 1"
		having = " HAVING COUNT(p.id) > 0"
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+categoryCols+", COUNT(p.id) FROM categories c "+join+
		" GROUP BY c.id"+having+" ORDER BY c.position, c.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var (
			c                Category
			created, updated string
		)
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.Position, &created, &updated, &c.Count); err != nil {
			return nil, err
		}
		c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReorderCategories sets each category's position to its index in ids.
func (s *Store) ReorderCategories(ctx context.Context, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE categories SET position = ? WHERE id = ?", i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteCategory removes the category; its paintings become uncategorised.
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM categories WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
