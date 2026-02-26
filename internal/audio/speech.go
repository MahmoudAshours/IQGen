package audio

import (
	"context"
	"fmt"
	"time"

	"qgencodex/internal/ffmpeg"
)

const defaultSpeechEnhanceFilter = "highpass=f=120,lowpass=f=3800,afftdn=nf=-25,acompressor=threshold=-18dB:ratio=2:attack=20:release=250,alimiter=limit=0.95"

// EnhanceForSpeechRecognition applies a speech-focused cleanup filter chain and
// outputs mono 16kHz WAV to improve ASR robustness in noisy/echo-heavy audio.
func EnhanceForSpeechRecognition(ctx context.Context, inputPath, outputPath, filter string) error {
	if inputPath == "" || outputPath == "" {
		return fmt.Errorf("input and output paths are required")
	}
	if filter == "" {
		filter = defaultSpeechEnhanceFilter
	}
	args := []string{
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-af", filter,
		"-c:a", "pcm_s16le",
		outputPath,
	}
	return ffmpeg.Run(ctx, args...)
}

// TrimFrom removes audio before startAt and writes the remainder as MP3.
func TrimFrom(ctx context.Context, inputPath, outputPath string, startAt time.Duration, bitrateKbps int) error {
	if inputPath == "" || outputPath == "" {
		return fmt.Errorf("input and output paths are required")
	}
	if startAt < 0 {
		startAt = 0
	}
	if bitrateKbps <= 0 {
		bitrateKbps = 128
	}
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", startAt.Seconds()),
		"-i", inputPath,
		"-vn",
		"-c:a", "libmp3lame",
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		outputPath,
	}
	return ffmpeg.Run(ctx, args...)
}
