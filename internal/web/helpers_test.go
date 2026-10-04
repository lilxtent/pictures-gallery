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
