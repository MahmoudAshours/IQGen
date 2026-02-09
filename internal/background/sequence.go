package background

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qgencodex/internal/ffmpeg"
	"qgencodex/internal/utils"
)

type Segment struct {
	Text           string
	Duration       time.Duration
	MinDurationSec int
}

// BuildSequence downloads a background per segment and concatenates them.
func BuildSequence(ctx context.Context, selector *Selector, segments []Segment, width, height int, tempDir, outputPath string) error {
	if selector == nil {
		return fmt.Errorf("background selector is nil")
	}
	if len(segments) == 0 {
		return fmt.Errorf("no segments provided")
	}
	if err := utils.EnsureDir(tempDir); err != nil {
		return err
	}
	bgDir := filepath.Join(tempDir, "background_seq")
	if err := utils.EnsureDir(bgDir); err != nil {
		return err
	}
	var segmentFiles []string
	for i, seg := range segments {
		if seg.Duration <= 0 {
			continue
		}
		origMin := selector.MinDuration
		if seg.MinDurationSec > selector.MinDuration {
			selector.MinDuration = seg.MinDurationSec
		}
		bgPath := filepath.Join(bgDir, fmt.Sprintf("bg_%d.mp4", i))
		_, err := selector.SelectAndDownload(ctx, seg.Text, bgPath)
		selector.MinDuration = origMin
		if err != nil {
			return err
		}
		segmentPath := filepath.Join(bgDir, fmt.Sprintf("segment_%d.mp4", i))
		err = buildSegment(ctx, bgPath, segmentPath, width, height, seg.Duration)
		if err != nil {
			return err
		}
		segmentFiles = append(segmentFiles, segmentPath)
	}
	if len(segmentFiles) == 0 {
		return fmt.Errorf("no background segments created")
	}
	return concatSegments(ctx, segmentFiles, outputPath, bgDir)
}

func buildSegment(ctx context.Context, inputPath, outputPath string, width, height int, duration time.Duration) error {
	args := []string{
		"-y",
		"-stream_loop", "-1",
		"-i", inputPath,
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", width, height, width, height),
		"-r", "30",
		"-an",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "20",
		"-pix_fmt", "yuv420p",
		outputPath,
	}
	return ffmpeg.Run(ctx, args...)
}

func concatSegments(ctx context.Context, segments []string, outputPath, tempDir string) error {
	listPath := filepath.Join(tempDir, "background_concat.txt")
	absListPath, err := filepath.Abs(listPath)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, seg := range segments {
		segPath := seg
		if !filepath.IsAbs(segPath) {
			absSeg, err := filepath.Abs(segPath)
			if err == nil {
				segPath = absSeg
			}
		}
		b.WriteString("file '")
		b.WriteString(strings.ReplaceAll(segPath, "'", "'\\''"))
		b.WriteString("'\n")
	}
	if err := os.WriteFile(absListPath, []byte(b.String()), 0o644); err != nil {
		return err
	}
	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", absListPath,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "20",
		"-pix_fmt", "yuv420p",
		"-an",
		outputPath,
	}
	return ffmpeg.Run(ctx, args...)
}
