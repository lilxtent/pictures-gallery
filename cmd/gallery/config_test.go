package main

import (
	"strings"
	"testing"
)

func getenvFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestBaseURLRequiredOutsideDev(t *testing.T) {
	cfg := loadConfig(getenvFrom(map[string]string{}))
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "BASE_URL must be set in production") {
		t.Fatalf("err = %v, want BASE_URL error", err)
	}
}

func TestBaseURLDefaultsInDev(t *testing.T) {
	cfg := loadConfig(getenvFrom(map[string]string{"DEV": "1"}))
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestBaseURLFromEnvIsTrimmed(t *testing.T) {
	cfg := loadConfig(getenvFrom(map[string]string{"BASE_URL": "https://example.ru/"}))
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example.ru" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}
