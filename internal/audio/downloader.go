package audio

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

type quranAPIAudioEntry struct {
	Reciter     string `json:"reciter"`
	URL         string `json:"url"`
	OriginalURL string `json:"originalUrl"`
}

var surahAyahCounts = [...]int{
	7, 286, 200, 176, 120, 165, 206, 75, 129, 109, 123, 111, 43, 52, 99, 128, 111,
	110, 98, 135, 112, 78, 118, 64, 77, 227, 93, 88, 69, 60, 34, 30, 73, 54, 45, 83,
	182, 88, 75, 85, 54, 53, 89, 59, 37, 35, 38, 29, 18, 45, 60, 49, 62, 55, 78, 96,
	29, 22, 24, 13, 14, 11, 11, 18, 12, 12, 30, 52, 52, 44, 28, 28, 20, 56, 40, 31,
	50, 40, 46, 42, 29, 19, 36, 25, 22, 17, 19, 26, 30, 20, 15, 21, 11, 8, 8, 19, 5,
	8, 8, 11, 11, 8, 3, 9, 5, 4, 7, 3, 6, 3, 5, 4, 5, 6,
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
			err := d.downloadAyahWithFallback(ctx, client, number, path)
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

func (d *Downloader) downloadAyahWithFallback(ctx context.Context, client *http.Client, ayahNumber int, destPath string) error {
	sources := d.audioSourcesForAyah(ctx, client, ayahNumber)
	if len(sources) == 0 {
		return fmt.Errorf("no audio sources available")
	}
	failures := make([]string, 0, len(sources))
	for _, source := range sources {
		err := retry.Do(ctx, 2, 250*time.Millisecond, func() error {
			if err := utils.DownloadFile(ctx, client, source, nil, destPath); err != nil {
				return err
			}
			return validateDownloadedAudio(destPath, ayahNumber, source, d.Reciter)
		})
		if err == nil {
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s (%v)", source, err))
	}
	return fmt.Errorf("all audio sources failed: %s", strings.Join(failures, "; "))
}

func (d *Downloader) audioSourcesForAyah(ctx context.Context, client *http.Client, ayahNumber int) []string {
	urls := []string{
		fmt.Sprintf("%s/%d/%s/%d.mp3", d.BaseURL, d.BitrateKbps, d.Reciter, ayahNumber),
	}
	surah, ayah, ok := globalAyahToSurahAyah(ayahNumber)
	if !ok {
		return dedupeNonEmpty(urls)
	}
	if fallback := everyAyahURLForReciter(d.Reciter, surah, ayah); fallback != "" {
		urls = append(urls, fallback)
	}
	urls = append(urls, quranAPIAudioURLs(ctx, client, surah, ayah, d.Reciter)...)
	return dedupeNonEmpty(urls)
}

func validateDownloadedAudio(path string, ayahNumber int, url string, reciter string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= 0 {
		return fmt.Errorf("empty audio file from CDN for ayah %d (reciter=%s, url=%s)", ayahNumber, reciter, url)
	}
	return nil
}

func globalAyahToSurahAyah(globalAyah int) (int, int, bool) {
	if globalAyah <= 0 {
		return 0, 0, false
	}
	remaining := globalAyah
	for i, count := range surahAyahCounts {
		if remaining <= count {
			return i + 1, remaining, true
		}
		remaining -= count
	}
	return 0, 0, false
}

func everyAyahURLForReciter(reciter string, surah int, ayah int) string {
	folder := everyAyahReciterFolder(reciter)
	if folder == "" {
		return ""
	}
	return fmt.Sprintf("https://everyayah.com/data/%s/%03d%03d.mp3", folder, surah, ayah)
}

func everyAyahReciterFolder(reciter string) string {
	r := strings.ToLower(strings.TrimSpace(reciter))
	switch r {
	case "ar.alafasy":
		return "Alafasy_128kbps"
	case "ar.shaatree":
		return "Abu_Bakr_Ash-Shaatree_128kbps"
	case "ar.husary":
		return "Husary_128kbps"
	case "ar.muhammadjibreel":
		return "Muhammad_Jibreel_128kbps"
	case "ar.abdurrahmaansudais":
		return "Abdurrahmaan_As-Sudais_192kbps"
	default:
		return ""
	}
}

func quranAPIAudioURLs(ctx context.Context, client *http.Client, surah int, ayah int, reciter string) []string {
	endpoint := fmt.Sprintf("https://quranapi.pages.dev/api/audio/%d/%d.json", surah, ayah)
	var payload map[string]quranAPIAudioEntry
	if err := utils.GetJSON(ctx, client, endpoint, nil, &payload); err != nil || len(payload) == 0 {
		return nil
	}
	entry, ok := pickPreferredQuranAPIAudio(payload, reciter)
	if !ok {
		return nil
	}
	return dedupeNonEmpty([]string{entry.OriginalURL, entry.URL})
}

func pickPreferredQuranAPIAudio(entries map[string]quranAPIAudioEntry, reciter string) (quranAPIAudioEntry, bool) {
	if len(entries) == 0 {
		return quranAPIAudioEntry{}, false
	}
	aliases := reciterAliases(reciter)
	for _, alias := range aliases {
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Reciter), alias) {
				return e, true
			}
		}
	}
	if e, ok := entries["1"]; ok {
		return e, true
	}
	keys := make([]int, 0, len(entries))
	for k := range entries {
		n, err := strconv.Atoi(k)
		if err == nil {
			keys = append(keys, n)
		}
	}
	sort.Ints(keys)
	for _, k := range keys {
		if e, ok := entries[strconv.Itoa(k)]; ok {
			return e, true
		}
	}
	for _, e := range entries {
		return e, true
	}
	return quranAPIAudioEntry{}, false
}

func reciterAliases(reciter string) []string {
	r := strings.ToLower(strings.TrimSpace(reciter))
	switch {
	case strings.Contains(r, "alafasy") || strings.Contains(r, "afasy"):
		return []string{"afasy", "mishary"}
	case strings.Contains(r, "shaatree") || strings.Contains(r, "shatri"):
		return []string{"shatri", "shaatree", "abu bakr"}
	case strings.Contains(r, "jibreel") || strings.Contains(r, "jibril"):
		return []string{"jibreel", "jibril"}
	case strings.Contains(r, "dosari") || strings.Contains(r, "dussary"):
		return []string{"dosari", "dussary"}
	case strings.Contains(r, "rifai"):
		return []string{"rifai"}
	default:
		return []string{r}
	}
}

func dedupeNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		s := strings.TrimSpace(v)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
