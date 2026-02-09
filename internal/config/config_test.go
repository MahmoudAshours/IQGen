package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigValidates(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected default config to validate, got %v", err)
	}
}

func TestValidateBackgroundQuality(t *testing.T) {
	cfg := Default()
	cfg.Background.Quality = "ultra"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for unsupported background quality")
	}
}

func TestLoadOrCreateCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if cfg == nil {
		t.Fatalf("expected config")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_API", "https://example.com")
	cfg := Default()
	cfg.QuranAPI.BaseURL = "${TEST_API}"
	cfg.ExpandEnv()
	if cfg.QuranAPI.BaseURL != "https://example.com" {
		t.Fatalf("expected env expansion, got %s", cfg.QuranAPI.BaseURL)
	}
}

func TestValidateRenderer(t *testing.T) {
	cfg := Default()
	cfg.Video.Renderer = "unknown"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for unsupported renderer")
	}
}
