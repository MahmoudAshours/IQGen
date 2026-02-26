package render

import (
	"strings"
	"testing"
	"time"

	"qgencodex/internal/config"
	"qgencodex/internal/quran"
)

func TestBuildASSContent(t *testing.T) {
	cfg := config.Default().Video
	cfg.Font.Family = "Amiri Quran"
	opts := assOptions{
		Width:  1080,
		Height: 1920,
		Mode:   "sequential",
		Timings: []Timing{
			{Verse: quran.Verse{Text: "بسم الله"}, Start: 0, End: 1 * time.Second},
		},
		Config: cfg,
	}
	content := buildASSContent(opts)
	if !strings.Contains(content, "[Events]") {
		t.Fatalf("expected events section")
	}
	if !strings.Contains(content, "Dialogue:") {
		t.Fatalf("expected dialogue line")
	}
	if !strings.Contains(content, "0:00:00.00,0:00:01.00") {
		t.Fatalf("expected timing in dialogue")
	}
}

func TestASSFadeOverride(t *testing.T) {
	cfg := config.Default().Video
	cfg.FadeInMs = 100
	cfg.FadeOutMs = 200
	opts := assOptions{
		Width:  1080,
		Height: 1920,
		Mode:   "sequential",
		Timings: []Timing{
			{Verse: quran.Verse{Text: "بسم الله"}, Start: 0, End: 1 * time.Second},
		},
		Config: cfg,
	}
	content := buildASSContent(opts)
	if !strings.Contains(content, "\\fad(100,200)") {
		t.Fatalf("expected fade override in ASS content")
	}
}

func TestBuildASSContentLinesMode(t *testing.T) {
	cfg := config.Default().Video
	cfg.Font.Family = "Amiri Quran"
	opts := assOptions{
		Width:  1280,
		Height: 720,
		Mode:   "lines",
		Timings: []Timing{
			{Verse: quran.Verse{Text: "بسم الله الرحمن الرحيم"}, Start: 0, End: 2 * time.Second},
		},
		Config: cfg,
	}
	content := buildASSContent(opts)
	if !strings.Contains(content, "Dialogue: 0,0:00:00.00,0:00:02.00") {
		t.Fatalf("expected line dialogue in lines mode")
	}
}
