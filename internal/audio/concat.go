package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"qgencodex/internal/ffmpeg"
	"qgencodex/internal/utils"
)

// Concat concatenates audio segments into a single output using ffmpeg concat demuxer.
func Concat(ctx context.Context, segments []Segment, outputPath string, tempDir string) error {
	if len(segments) == 0 {
		return fmt.Errorf("no audio segments to concatenate")
	}
	if err := utils.EnsureDir(tempDir); err != nil {
		return err
	}
	listPath := filepath.Join(tempDir, "audio_concat.txt")
	absListPath, err := filepath.Abs(listPath)
	if err != nil {
		return err
	}
	content, err := concatListContent(segments)
	if err != nil {
		return err
	}
	if err := os.WriteFile(absListPath, []byte(content), 0o644); err != nil {
		return err
	}
	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", absListPath,
		"-c", "copy",
		outputPath,
	}
	return ffmpeg.Run(ctx, args...)
}

func escapeConcatPath(path string) string {
	return strings.ReplaceAll(path, "'", "'\\''")
}

func concatListContent(segments []Segment) (string, error) {
	var b strings.Builder
	for _, seg := range segments {
		segPath := seg.Path
		if !filepath.IsAbs(segPath) {
			absSeg, err := filepath.Abs(segPath)
			if err != nil {
				return "", err
			}
			segPath = absSeg
		}
		b.WriteString("file '")
		b.WriteString(escapeConcatPath(segPath))
		b.WriteString("'\n")
	}
	return b.String(), nil
}
