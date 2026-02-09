package caption

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qgencodex/internal/quran"
	"qgencodex/internal/render"
)

func TestWriteSRT(t *testing.T) {
	timings := []render.Timing{
		{Verse: quran.Verse{Text: "A"}, Start: 0, End: 1 * time.Second},
		{Verse: quran.Verse{Text: "B", Translation: "Bee"}, Start: 1 * time.Second, End: 2 * time.Second},
	}
	path := filepath.Join(t.TempDir(), "captions.srt")
	if err := WriteSRT(path, timings, true); err != nil {
		t.Fatalf("WriteSRT failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "1\n00:00:00,000 --> 00:00:01,000\nA") {
		t.Fatalf("unexpected content: %s", content)
	}
	if !strings.Contains(content, "B\nBee") {
		t.Fatalf("expected translation")
	}
}
