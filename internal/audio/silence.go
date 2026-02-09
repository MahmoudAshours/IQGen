package audio

import (
	"context"
	"fmt"

	"qgencodex/internal/ffmpeg"
)

// TrimSilence removes leading and trailing silence using ffmpeg silenceremove.
func TrimSilence(ctx context.Context, inputPath, outputPath string, bitrate int, silenceDB int, silenceSec float64) error {
	if silenceDB == 0 {
		silenceDB = -35
	}
	if silenceSec == 0 {
		silenceSec = 0.3
	}
	filter := fmt.Sprintf("silenceremove=start_periods=1:start_duration=%.2f:start_threshold=%ddB:stop_periods=1:stop_duration=%.2f:stop_threshold=%ddB",
		silenceSec, silenceDB, silenceSec, silenceDB)
	args := []string{
		"-y",
		"-i", inputPath,
		"-af", filter,
		"-b:a", fmt.Sprintf("%dk", bitrate),
		outputPath,
	}
	return ffmpeg.Run(ctx, args...)
}
