CREATE TABLE paintings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
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
