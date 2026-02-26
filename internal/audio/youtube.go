package audio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DownloadYouTubeAudio downloads audio from a YouTube URL and returns the local MP3 path.
func DownloadYouTubeAudio(ctx context.Context, videoURL, destPath, ytDlpCmd string) (string, error) {
	if strings.TrimSpace(videoURL) == "" {
		return "", fmt.Errorf("youtube url is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return "", fmt.Errorf("destination path is required")
	}
	cmdName := strings.TrimSpace(ytDlpCmd)
	if cmdName == "" {
		cmdName = "yt-dlp"
	}
	if _, err := exec.LookPath(cmdName); err != nil {
		return "", fmt.Errorf("%s not found in PATH", cmdName)
	}
	base := strings.TrimSuffix(destPath, filepath.Ext(destPath))
	if base == "" {
		base = destPath
	}
	template := base + ".%(ext)s"
	args := []string{
		"-f", "bestaudio/best",
		"--no-playlist",
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"-o", template,
		videoURL,
	}
	cmd := exec.CommandContext(ctx, cmdName, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", cmdName, err, strings.TrimSpace(out.String()))
	}
	mp3Path := base + ".mp3"
	if _, err := os.Stat(mp3Path); err == nil {
		return mp3Path, nil
	}
	matches, _ := filepath.Glob(base + ".*")
	for _, m := range matches {
		if strings.HasSuffix(strings.ToLower(m), ".mp3") {
			return m, nil
		}
	}
	return "", fmt.Errorf("downloaded audio file not found for template %s", template)
}
