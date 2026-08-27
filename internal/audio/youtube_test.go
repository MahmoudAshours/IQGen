package audio

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadYouTubeAudioRequiresURL(t *testing.T) {
	_, err := DownloadYouTubeAudio(context.Background(), "", "/tmp/out.mp3", "yt-dlp")
	if err == nil {
		t.Fatalf("expected error for empty url")
	}
}

func TestDownloadYouTubeAudioWithStubCommand(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "yt-dlp-stub.sh")
	script := `#!/bin/sh
out=""
force_overwrites=0
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--force-overwrites" ]; then
    force_overwrites=1
  fi
  if [ "$1" = "-o" ]; then
    out="$2"
    shift 2
    continue
  fi
  shift
done
if [ "$force_overwrites" -ne 1 ]; then
  exit 1
fi
outfile=$(printf "%s" "$out" | sed 's/%(ext)s/mp3/g')
echo "audio" > "$outfile"
`
	if err := os.WriteFile(cmdPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	dest := filepath.Join(dir, "downloaded.mp3")
	got, err := DownloadYouTubeAudio(context.Background(), "https://youtube.com/watch?v=test", dest, cmdPath)
	if err != nil {
		t.Fatalf("DownloadYouTubeAudio failed: %v", err)
	}
	if got == "" {
		t.Fatalf("expected output path")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
}

func TestDownloadYouTubeAudioOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "yt-dlp-stub.sh")
	script := `#!/bin/sh
out=""
force_overwrites=0
url=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--force-overwrites" ]; then
    force_overwrites=1
    shift
    continue
  fi
  if [ "$1" = "-o" ]; then
    out="$2"
    shift 2
    continue
  fi
  url="$1"
  shift
done
if [ "$force_overwrites" -ne 1 ]; then
  exit 1
fi
outfile=$(printf "%s" "$out" | sed 's/%(ext)s/mp3/g')
printf "%s" "$url" > "$outfile"
`
	if err := os.WriteFile(cmdPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	dest := filepath.Join(dir, "downloaded.mp3")
	if err := os.WriteFile(dest, []byte("stale-audio"), 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}
	if _, err := DownloadYouTubeAudio(context.Background(), "https://example.com/first", dest, cmdPath); err != nil {
		t.Fatalf("first download failed: %v", err)
	}
	got, err := DownloadYouTubeAudio(context.Background(), "https://example.com/second", dest, cmdPath)
	if err != nil {
		t.Fatalf("second download failed: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read overwritten file: %v", err)
	}
	if string(data) != "https://example.com/second" {
		t.Fatalf("expected final file to contain second URL, got %q", data)
	}
	t.Logf("overwritten file now contains %q", data)
}
