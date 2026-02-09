package audio

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"qgencodex/internal/ffmpeg"
	"qgencodex/internal/retry"
	"qgencodex/internal/utils"
)

type Segment struct {
	AyahNumber int
	Path       string
	Duration   time.Duration
}

type Downloader struct {
	BaseURL       string
	Reciter       string
	BitrateKbps   int
	Timeout       time.Duration
	MaxConcurrent int
	RemoveSilence bool
	SilenceDB     int
	SilenceSec    float64
}

func (d *Downloader) DownloadSegments(ctx context.Context, ayahNumbers []int, destDir string) ([]Segment, error) {
	if d.MaxConcurrent <= 0 {
		d.MaxConcurrent = 3
	}
	client := utils.HTTPClient(d.Timeout)
	segments := make([]Segment, len(ayahNumbers))
	errs := make(chan error, len(ayahNumbers))
	sem := make(chan struct{}, d.MaxConcurrent)
	var wg sync.WaitGroup

	for i, ayah := range ayahNumbers {
		wg.Add(1)
		go func(idx int, number int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			filename := fmt.Sprintf("%d.mp3", number)
			path := filepath.Join(destDir, filename)
			url := fmt.Sprintf("%s/%d/%s/%d.mp3", d.BaseURL, d.BitrateKbps, d.Reciter, number)
			err := retry.Do(ctx, 3, 300*time.Millisecond, func() error {
				return utils.DownloadFile(ctx, client, url, nil, path)
			})
			if err != nil {
				errs <- fmt.Errorf("download ayah %d: %w", number, err)
				return
			}
			if d.RemoveSilence {
				trimmed := filepath.Join(destDir, fmt.Sprintf("%d_trim.mp3", number))
				if err := TrimSilence(ctx, path, trimmed, d.BitrateKbps, d.SilenceDB, d.SilenceSec); err == nil {
					path = trimmed
				}
			}
			dur, err := ffmpeg.ProbeDuration(ctx, path)
			if err != nil {
				errs <- fmt.Errorf("probe duration for ayah %d: %w", number, err)
				return
			}
			segments[idx] = Segment{
				AyahNumber: number,
				Path:       path,
				Duration:   time.Duration(dur * float64(time.Second)),
			}
		}(i, ayah)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return segments, nil
}
