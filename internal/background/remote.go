package background

import (
	"context"
	"fmt"
	"time"

	"qgencodex/internal/ffmpeg"
)

// DownloadURLSegment downloads only the needed duration of a direct video URL.
func DownloadURLSegment(ctx context.Context, url, destPath string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("invalid duration")
	}
	args := []string{
		"-y",
		"-ss", "0",
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-i", url,
		"-an",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "20",
		"-pix_fmt", "yuv420p",
		destPath,
	}
	return ffmpeg.Run(ctx, args...)
}
