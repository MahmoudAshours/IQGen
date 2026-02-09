package render

import (
	"strings"
	"testing"
	"unicode"

	"qgencodex/internal/config"
)

func TestWrapTextSplitsLongLine(t *testing.T) {
	text := "one two three four five"
	lines := wrapText(text, 80, 20)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %v", lines)
	}
}

func TestWrapTextDoesNotStartWithCombining(t *testing.T) {
	text := "بِسْمِ اللَّهِ"
	lines := wrapText(text, 30, 30)
	for _, line := range lines {
		for i, r := range line {
			if i == 0 && unicode.Is(unicode.Mn, r) {
				t.Fatalf("line starts with combining mark: %q", line)
			}
			break
		}
	}
}

func TestElongateTextUnderscore(t *testing.T) {
	got := elongateText("ي__ا")
	if !strings.Contains(got, "ـ") {
		t.Fatalf("expected tatweel, got %q", got)
	}
}

func TestMaybeElongateLines(t *testing.T) {
	cfg := config.Default().Video
	cfg.Elongate = true
	lines := []string{"يا الله"}
	out := maybeElongateLines(cfg, lines, 600, 32)
	if len(out) != 1 {
		t.Fatalf("expected 1 line, got %d", len(out))
	}
	if !strings.Contains(out[0], "ـ") {
		t.Fatalf("expected elongation, got %q", out[0])
	}
}
