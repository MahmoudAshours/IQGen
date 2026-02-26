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
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    out="$2"
    shift 2
    continue
  fi
  shift
done
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
