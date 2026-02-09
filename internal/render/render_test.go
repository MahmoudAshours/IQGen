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
