package background

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DownloadYouTube downloads a YouTube video to destPath using yt-dlp.
func DownloadYouTube(ctx context.Context, videoURL, destPath string) error {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return fmt.Errorf("yt-dlp not found in PATH")
	}
	args := []string{
		"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"-o", destPath,
		videoURL,
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yt-dlp failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// DownloadYouTubeSegment downloads a segment of a YouTube video using yt-dlp.
func DownloadYouTubeSegment(ctx context.Context, videoURL, destPath string, duration time.Duration) error {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return fmt.Errorf("yt-dlp not found in PATH")
	}
	segment := fmt.Sprintf("*00:00:00-%s", formatYTDLDuration(duration))
	args := []string{
		"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"--download-sections", segment,
		"--force-keyframes-at-cuts",
		"-o", destPath,
		videoURL,
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yt-dlp failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func formatYTDLDuration(d time.Duration) string {
	if d <= 0 {
		return "00:00:00"
	}
	total := int(d.Round(time.Second).Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
