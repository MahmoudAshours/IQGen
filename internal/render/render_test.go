package render

import (
	"testing"
	"time"

	"qgencodex/internal/config"
)

func TestFadeAlphaExprDisabled(t *testing.T) {
	cfg := config.Default().Video
	cfg.FadeInMs = 0
	cfg.FadeOutMs = 0
	got := fadeAlphaExpr(cfg, 0, 1*time.Second)
	if got != "" {
		t.Fatalf("expected empty alpha expr, got %q", got)
	}
}

func TestFadeAlphaExprEnabled(t *testing.T) {
	cfg := config.Default().Video
	cfg.FadeInMs = 100
	cfg.FadeOutMs = 100
	got := fadeAlphaExpr(cfg, 0, 2*time.Second)
	if got == "" {
		t.Fatalf("expected alpha expr")
	}
}

func TestIsImagePath(t *testing.T) {
	for _, path := range []string{"background.jpg", "background.JPEG", "background.png", "background.webp"} {
		if !isImagePath(path) {
			t.Fatalf("expected image path: %s", path)
		}
	}
	if isImagePath("background.mp4") {
		t.Fatal("did not expect video path to be an image")
	}
}
