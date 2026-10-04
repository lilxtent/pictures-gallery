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

	"github.com/lilxtent/pictures-gallery/internal/admin"
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
