# Painter portfolio site — design

Date: 2026-10-04
Status: approved in brainstorming, pending spec review

## Goal

A Russian-language portfolio website for a watercolour painter (the author's mother).
Visitors browse her paintings, read the story behind each one, learn about her and
find her contacts. She manages all content herself through a simple admin panel,
mostly from her phone.

Reference site she liked: https://cpx-gallery.ru/efilenka/ (blog-style feed of
paintings with long personal descriptions, an "about" block, contacts).

## Requirements

- Public site in Russian, works well on phones and desktops.
- Portfolio only: no prices, no sales status, no cart or payments.
- Contacts are displayed as links only: no contact form, no personal data collected
  (keeps 152-FZ obligations minimal). No analytics or cookies for visitors.
- The painter edits everything herself: she is comfortable with a phone/laptop but
  not technical.
- One photo per painting.
- Hosted on a Russian VPS with a `.ru` domain (visitors mostly in Russia).

Out of scope (YAGNI, can be added later): categories/series, comments, likes,
watermarks, multiple photos per painting, multilingual content, Yandex Metrica,
automatic deploy on push.

## Architecture

A single Go binary serves both the public site and the admin.

- Go 1.22+ standard library: `net/http` (pattern routing), `html/template`, `log/slog`, `embed`.
- SQLite via `modernc.org/sqlite` (pure Go, no cgo). One file: `data/gallery.db`.
- Images: `github.com/disintegration/imaging` for decode/orient/crop/rotate/resize;
  `golang.org/x/image/webp` for WebP decoding. Output is JPEG.
- Templates, CSS, JS and fonts are embedded in the binary.
- Front end: server-rendered HTML, plain CSS, small vanilla JS (lightbox, admin).
  Two vendored JS libraries in the admin only: Cropper.js (crop/rotate) and
  SortableJS (drag-to-reorder, touch-friendly). Fonts (Lora, Marck Script, both OFL)
  are self-hosted. Nothing is loaded from external CDNs, which can be slow or
  blocked in Russia.
- All mutable state lives in one `data/` directory (database + images).

### Suggested layout

```
cmd/gallery/          main: config, wiring, server start
internal/store/       SQLite access: paintings, settings, sessions; migrations
internal/images/      upload processing: decode, orient, crop, rotate, resize, strip metadata
internal/slug/        Russian → Latin transliteration for URLs
internal/web/         public handlers + templates
internal/admin/       admin handlers, auth, CSRF + templates
web/static/           CSS, JS, fonts, vendored libs (embedded)
deploy/               Dockerfile, docker-compose.yml, Caddyfile, backup script, deploy.sh
```

Each package has one job and is testable on its own: `store` knows nothing about
HTTP, `images` knows nothing about the database, handlers depend on small interfaces.

## Public site

Visual style "Watercolour paper" (option C from the mockups):
- Warm paper background (~`#efe8dc`), dark warm-brown text.
- Artist name in a handwritten script (Marck Script); body text in Lora.
- Each painting is shown inside a white passe-partout (mat) with a soft shadow.
- Captions in italic muted brown; dashed dividers.

Header on every page: name, subtitle (e.g. «художник, акварель»), menu
*Работы · Об авторе · Контакты*. Footer on every page: her contacts.

### `/` — Работы
- Optional greeting block at the top: small round author photo + a few lines of
  text + link «Подробнее обо мне →». Hidden when the greeting text is empty.
- Grid of visible paintings in her manual order: 3 columns on desktop,
  2 on tablet, 1 on phone. Images keep their natural aspect ratio (no cropping).
- Card: image in mat, title, one details line «Техника · Размер · Год»
  (empty parts are omitted).
- Images use `loading="lazy"` and `srcset`; no pagination.

### `/paintings/{slug}` — painting page
- Layout A "side by side": on desktop the painting (in mat) is on the left and
  sticky while the story scrolls on the right; on phone it stacks image → text.
- Title, details line, full description. Description is plain text: blank lines
  separate paragraphs, single line breaks are preserved. All text is HTML-escaped.
- Tapping the image opens a full-screen lightbox with the largest size.
- «← Все работы» link; previous/next links (by position, visible paintings only),
  labelled with the neighbouring painting's title.
- Hidden or unknown slug → 404.

### `/about` — Об авторе
Author photo (in mat) and bio text (same plain-text paragraph rules).

### `/contacts` — Контакты
Clickable links for whichever fields are filled in: phone (`tel:`), email
(`mailto:`), Telegram (`https://t.me/...`), WhatsApp (`https://wa.me/...`), VK.

### Sharing and search
- Each page has `<title>`, meta description and Open Graph / Twitter tags;
  painting pages use the painting image and title, so links in Telegram/VK show
  a preview.
- `/sitemap.xml` (home, about, contacts, visible paintings) and `/robots.txt`
  (disallows `/admin`).
- `lang="ru"` everywhere.

## Admin

Lives under `/admin`, Russian UI, large touch targets, works well on a phone.
All admin pages except login require a valid session.

### Login
- Single admin account, password only.
- Password stored as a bcrypt hash in `settings`. On first start, if no hash
  exists, it is set from the `ADMIN_PASSWORD` env var (startup fails with a clear
  message if neither exists).
- On success: random 32-byte session token in a cookie (`HttpOnly`, `Secure` in
  production, `SameSite=Lax`, 30 days). Only the SHA-256 of the token is stored.
- Rate limit: after 5 failed attempts from an IP within 15 minutes, further
  attempts are refused for 15 minutes (in-memory counter).
- Logout deletes the session.

### «Картины» (main screen)
- List of all paintings in site order: thumbnail, title, «Скрыта» badge when hidden.
- Drag to reorder (SortableJS); the new order is saved immediately via a POST.
- Button «+ Добавить картину».

### Add / edit painting
1. Choose photo (`<input type="file" accept="image/jpeg,image/png,image/webp">`;
   on iPhone this makes Safari convert HEIC to JPEG automatically).
2. Crop and rotate in the browser (Cropper.js). The original file and the crop
   parameters (x, y, width, height in original-pixel coordinates, rotation in
   multiples of 90°) are submitted; the server does the actual processing.
3. Form fields: Название (required), Техника, Размер, Год (optional, 1900 to current
   year), Описание, Показывать на сайте (checkbox, default on).
4. «Сохранить». New paintings get the top position.

Edit screen also has: «Изменить кадрирование» (re-crop from the stored original
without re-uploading), «Заменить фото», «Посмотреть на сайте», «Удалить» (with a
confirmation step; deletes the row and all its image files).
A `beforeunload` warning prevents leaving with unsaved changes.

### «Об авторе»
Author photo (same upload + crop flow) and bio text.

### «Настройки»
Name, subtitle, home-page greeting text, phone, email, Telegram username,
WhatsApp number, VK link, and password change (current password + new password
twice, minimum 8 characters).

### Errors
Validation messages in plain Russian next to the field, form keeps entered values.
Examples: «Укажите название», «Фото слишком большое (максимум 30 МБ)»,
«Не удалось прочитать фото. Попробуйте другой файл».

## Data model

```sql
paintings (
  id            INTEGER PRIMARY KEY,
  slug          TEXT NOT NULL UNIQUE,   -- set on create, never changed
  title         TEXT NOT NULL,
  technique     TEXT NOT NULL DEFAULT '',
  size          TEXT NOT NULL DEFAULT '',
  year          INTEGER,                 -- NULL = not specified
  description   TEXT NOT NULL DEFAULT '',
  visible       INTEGER NOT NULL DEFAULT 1,
  position      INTEGER NOT NULL,        -- lower = earlier on site
  crop_x, crop_y, crop_w, crop_h INTEGER NOT NULL,
  rotation      INTEGER NOT NULL DEFAULT 0,  -- 0, 90, 180, 270
  image_version INTEGER NOT NULL DEFAULT 1,
  image_width, image_height INTEGER NOT NULL, -- of processed image, for layout
  created_at, updated_at TEXT NOT NULL
)

settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)
  -- artist_name, subtitle, greeting, about_text, about_image_version,
  -- about crop params, phone, email, telegram, whatsapp, vk, password_hash

sessions (token_hash TEXT PRIMARY KEY, expires_at TEXT NOT NULL)
```

Schema migrations: numbered SQL files embedded in the binary, applied in order at
startup, tracked with `PRAGMA user_version`.

Slugs: Russian → Latin transliteration (simple fixed table, e.g. «Жёлтая лилия» →
`zheltaya-liliya`), lowercase, non-alphanumerics collapsed to `-`. On collision
append `-2`, `-3`, …. Empty result falls back to `kartina-{id}`.

## Images

Storage under `data/images/`:
```
paintings/{id}/original.<ext>          private, never served
paintings/{id}/v{version}-600.jpg      card
paintings/{id}/v{version}-1200.jpg     painting page
paintings/{id}/v{version}-2000.jpg     lightbox
about/original.<ext>, about/v{version}-{size}.jpg
```

Processing pipeline (`internal/images`):
1. Reject > 30 MB (enforced with `http.MaxBytesReader`).
2. Decode (JPEG/PNG/WebP); failure → validation error.
3. Apply EXIF orientation.
4. Rotate by `rotation`, then crop to the given rectangle (clamped to bounds).
5. Resize to max long edge 600 / 1200 / 2000 (never upscale), Lanczos.
6. Encode JPEG (quality ~85). Re-encoding drops all metadata, including GPS.

Re-crop or photo replacement increments `image_version`, writes new files, then
deletes the old version's files. Public image URLs include the version, so they
are served with `Cache-Control: public, max-age=31536000, immutable`.

## Operations

Configuration (env vars):
- `ADMIN_PASSWORD`: initial admin password (only used if none is stored)
- `DATA_DIR` (default `./data`)
- `BASE_URL`: absolute URL for Open Graph tags and sitemap, e.g. `https://example.ru`
- `ADDR` (default `:8080`)
- `DEV=1`: disables the `Secure` cookie flag, seeds sample content if the DB is empty

Deployment:
- Multi-stage Dockerfile → small runtime image with the single binary.
- `docker-compose.yml`: `app` (with `data/` volume) + `caddy` (automatic HTTPS
  for the `.ru` domain, reverse proxy to app, gzip/zstd).
- Russian VPS (e.g. Timeweb Cloud, ~300 ₽/month).
- `deploy/deploy.sh`: ssh to the server, `git pull`, `docker compose up -d --build`.

Backups: nightly cron on the host runs `deploy/backup.sh`: SQLite `VACUUM INTO` a
snapshot, then `rclone sync` of the snapshot plus `data/images/` to an
S3-compatible bucket at the same provider; keeps 30 daily snapshots.

Security summary:
- CSRF: per-session token in a hidden field on every admin form, checked on every
  admin POST.
- Secure cookies as above; bcrypt passwords; login rate limit.
- Upload size limit and image decode check.
- All output via `html/template` auto-escaping.
- Admin pages send `Cache-Control: no-store` and `X-Robots-Tag: noindex`.

Errors and logging:
- Friendly Russian 404 and 500 pages in the site style.
- `slog` structured logs to stdout (request method, path, status, duration;
  errors with context).

## Testing

- `internal/slug`: transliteration table, collisions, edge cases (empty, digits,
  punctuation, ё/й/ъ/ь).
- `internal/images`: orientation, rotation + crop correctness, resize bounds,
  no upscaling, metadata removed from output, invalid input rejected.
- `internal/store`: CRUD, ordering, reorder, settings, sessions/expiry,
  migrations, against a temp-dir SQLite file.
- HTTP (`httptest`): home lists only visible paintings in order; painting page
  renders and escapes; hidden/unknown slug → 404; prev/next correct; admin
  redirects to login when logged out; login success/failure/rate limit; CSRF
  rejection; upload → painting created with image files; delete removes files.
- Manual end-to-end check in a browser on desktop and phone sizes.
- GitHub Actions: `go vet` + `go test ./...` on every push.
- Local development: `DEV=1 go run ./cmd/gallery` starts with the three sample
  watercolours (koi carp, yellow lily, peony) as seed content. The original photos
  are committed under `seed/` and go through the normal upload pipeline with
  preset crop rectangles (removing the table edge, pen and camera stamp).
