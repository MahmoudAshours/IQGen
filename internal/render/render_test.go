package render

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qgencodex/internal/config"
	"qgencodex/internal/quran"
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

func TestRenderPairModeWithImageBackground(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for render integration test")
	}

	dir := t.TempDir()
	backgroundPath := filepath.Join(dir, "telegram-style-bg.png")
	audioPath := filepath.Join(dir, "demo-audio.mp3")
	outputPath := filepath.Join(dir, "pair-mode.mp4")

	makeBackground := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=#1f2c34:s=540x960:d=1", "-vf", "drawbox=x=25:y=40:w=490:h=160:color=#2a3942:t=fill,drawbox=x=70:y=770:w=400:h=120:color=#ffffff@0.18:t=fill", "-frames:v", "1", "-update", "1", backgroundPath)
	if out, err := makeBackground.CombinedOutput(); err != nil {
		t.Fatalf("create background: %v: %s", err, out)
	}
	makeAudio := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=550:duration=3", "-q:a", "2", audioPath)
	if out, err := makeAudio.CombinedOutput(); err != nil {
		t.Fatalf("create audio: %v: %s", err, out)
	}

	cfg := config.Default().Video
	cfg.Resolution = "540x960"
	cfg.Renderer = "drawtext"
	cfg.Font.Family = ""
	cfg.Font.File = ""
	cfg.Reference.Enabled = false
	timings := []Timing{
		{
			Start:       0,
			End:         1500 * time.Millisecond,
			Verse:       quran.Verse{NumberInSurah: 1, Text: "بسم الله", Translation: "In the name of Allah [1]", SurahMeta: quran.SurahMeta{EnglishName: "Al-Fatiha"}},
			WordTimings: []WordTiming{{Word: "بسم", Start: 0, End: 700 * time.Millisecond}, {Word: "الله", Start: 700 * time.Millisecond, End: 1500 * time.Millisecond}},
		},
		{
			Start:       1500 * time.Millisecond,
			End:         3 * time.Second,
			Verse:       quran.Verse{NumberInSurah: 2, Text: "الرحمن الرحيم", Translation: "The Entirely Merciful, the Especially Merciful [2]", SurahMeta: quran.SurahMeta{EnglishName: "Al-Fatiha"}},
			WordTimings: []WordTiming{{Word: "الرحمن", Start: 1500 * time.Millisecond, End: 2200 * time.Millisecond}, {Word: "الرحيم", Start: 2200 * time.Millisecond, End: 3 * time.Second}},
		},
	}

	if err := Render(context.Background(), RenderInput{
		Timings:            timings,
		AudioPath:          audioPath,
		BackgroundPath:     backgroundPath,
		OutputPath:         outputPath,
		TempDir:            filepath.Join(dir, "tmp"),
		Mode:               "pair",
		VideoConfig:        cfg,
		IncludeTranslation: false,
	}); err != nil {
		t.Fatalf("render pair-mode video: %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected rendered video: %v", err)
	}
	if evidenceDir := strings.TrimSpace(os.Getenv("NO_MISTAKES_EVIDENCE_DIR")); evidenceDir != "" {
		framePath := filepath.Join(evidenceDir, "random-pair-demo-frame.png")
		extractFrame := exec.Command("ffmpeg", "-y", "-ss", "1", "-i", outputPath, "-frames:v", "1", "-update", "1", framePath)
		if out, err := extractFrame.CombinedOutput(); err != nil {
			t.Fatalf("extract frame: %v: %s", err, out)
		}
		videoCopy := filepath.Join(evidenceDir, "random-pair-demo.mp4")
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("read rendered video: %v", err)
		}
		if err := os.WriteFile(videoCopy, data, 0o644); err != nil {
			t.Fatalf("copy rendered video to evidence dir: %v", err)
		}
		t.Logf("evidence video: %s", videoCopy)
		t.Logf("evidence frame: %s", framePath)
	}
}
