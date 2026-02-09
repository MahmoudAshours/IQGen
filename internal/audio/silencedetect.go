package audio

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Silence struct {
	Start time.Duration
	End   time.Duration
}

// DetectSilences returns silence intervals using ffmpeg's silencedetect.
func DetectSilences(ctx context.Context, audioPath string, silenceDB int, silenceSec float64) ([]Silence, error) {
	if silenceDB == 0 {
		silenceDB = -35
	}
	if silenceSec == 0 {
		silenceSec = 0.2
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH")
	}
	filter := fmt.Sprintf("silencedetect=noise=%ddB:d=%.2f", silenceDB, silenceSec)
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", audioPath,
		"-af", filter,
		"-f", "null",
		"-",
	)
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseSilenceLog(stderr.Bytes()), nil
}

func parseSilenceLog(data []byte) []Silence {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var (
		silences     []Silence
		currentStart *time.Duration
	)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "silence_start:"); idx != -1 {
			value := strings.TrimSpace(line[idx+len("silence_start:"):])
			if startSec, ok := parseFloat(value); ok {
				start := time.Duration(startSec * float64(time.Second))
				currentStart = &start
			}
			continue
		}
		if idx := strings.Index(line, "silence_end:"); idx != -1 {
			value := strings.TrimSpace(line[idx+len("silence_end:"):])
			parts := strings.Fields(value)
			if len(parts) > 0 {
				if endSec, ok := parseFloat(parts[0]); ok {
					end := time.Duration(endSec * float64(time.Second))
					start := end
					if currentStart != nil {
						start = *currentStart
					}
					if end > start {
						silences = append(silences, Silence{Start: start, End: end})
					}
				}
			}
			currentStart = nil
		}
	}
	return silences
}

func parseFloat(value string) (float64, bool) {
	value = strings.TrimSpace(strings.TrimRight(value, "|"))
	if value == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
