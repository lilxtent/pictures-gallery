CREATE TABLE categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL,
    position   INTEGER NOT NULL,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

ALTER TABLE paintings ADD COLUMN category_id INTEGER REFERENCES categories (id) ON DELETE SET NULL;

CREATE INDEX paintings_category ON paintings (category_id);
