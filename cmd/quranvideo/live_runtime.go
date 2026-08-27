package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"qgencodex/internal/align"
	"qgencodex/internal/audio"
	"qgencodex/internal/background"
	"qgencodex/internal/config"
	"qgencodex/internal/quran"
	"qgencodex/internal/recognize"
	"qgencodex/internal/render"
	"qgencodex/internal/utils"
)

func parseLiveMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "full", "full-ayah", "full-ayahs", "sequential":
		return "full", nil
	case "2-word", "2words", "two-word", "two-words", "two", "pair", "2x2":
		return "2-word", nil
	default:
		return "", fmt.Errorf("unsupported live mode: %s (expected full or 2-word)", mode)
	}
}

func runLive(opts liveOptions) error {
	cfg, created, err := loadConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	logger := utils.NewLogger(cfg.Logging.Level)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(opts.ConfigPath))
	}
	if cfg.QuranAPI.UseLocalDB {
		logger.Infof("Quran source: local DB (%s)", cfg.QuranAPI.LocalDBPath)
	}
	aligner := newAligner(cfg.Audio)
	if !aligner.Available() {
		return sttUnavailableError(cfg.Audio)
	}
	ctx := context.Background()
	quranClient := newVerseSource(cfg.QuranAPI)
	var verses []quran.Verse
	if opts.HasFixedRange {
		verses, err = quranClient.FetchVerses(ctx, opts.Surah, opts.StartAyah, opts.EndAyah, cfg.QuranAPI.Edition, "")
		if err != nil {
			return err
		}
		if len(verses) == 0 {
			return fmt.Errorf("no verses found for surah %d %d-%d", opts.Surah, opts.StartAyah, opts.EndAyah)
		}
	}
	if !opts.Stream {
		if opts.Output == "" {
			if opts.HasFixedRange {
				name := fmt.Sprintf("live_surah%d_%d-%d_%s.mp4", opts.Surah, opts.StartAyah, opts.EndAyah, strings.ReplaceAll(opts.Mode, " ", "-"))
				opts.Output = filepath.Join(cfg.Output.Dir, name)
			} else {
				name := fmt.Sprintf("live_stream_%s.mp4", strings.ReplaceAll(opts.Mode, " ", "-"))
				opts.Output = filepath.Join(cfg.Output.Dir, name)
			}
		}
	}
	if err := utils.EnsureDir(cfg.Output.TempDir); err != nil {
		return err
	}
	if !opts.Stream {
		if err := utils.EnsureDir(filepath.Dir(opts.Output)); err != nil {
			return err
		}
	}

	runStamp := time.Now().Format("20060102_150405")
	liveDir := filepath.Join(cfg.Output.TempDir, "live_"+runStamp)
	chunksDir := filepath.Join(liveDir, "chunks")
	if err := utils.EnsureDir(chunksDir); err != nil {
		return err
	}
	liveTextPath := filepath.Join(liveDir, "live_text.txt")
	initialText := "Listening..."
	if len(verses) > 0 {
		initialText = verses[0].Text
	}
	if err := writeLiveTextAtomic(liveTextPath, initialText); err != nil {
		return err
	}

	width, height, err := render.ParseResolution(cfg.Video.Resolution)
	if err != nil {
		return err
	}
	bgPath := ""
	if opts.BackgroundPath != "" {
		bgPath, err = resolveBackgroundInput(ctx, opts.BackgroundPath, cfg.Output.TempDir, time.Duration(cfg.QuranAPI.TimeoutSec)*time.Second, logger, opts.Duration)
		if err != nil {
			return err
		}
	}
	args := []string{"-y"}
	if bgPath == "" || opts.NoBackground {
		color := cfg.Video.Background.Color
		if color == "" {
			color = "#000000"
		}
		colorSrc := fmt.Sprintf("color=c=%s:s=%dx%d:d=%.3f", color, width, height, opts.Duration.Seconds())
		args = append(args, "-f", "lavfi", "-i", colorSrc)
	} else {
		args = append(args, "-stream_loop", "-1", "-i", bgPath)
	}
	audioInput, sourceLabel, err := resolveLiveAudioInputArgs(ctx, opts)
	if err != nil {
		return err
	}
	args = append(args, audioInput...)

	fontSize := cfg.Video.Font.Size
	if fontSize <= 0 {
		fontSize = 64
	}
	textColor := cfg.Video.Font.Color
	if textColor == "" {
		textColor = "#FFFFFF"
	}
	draw := render.DrawtextArgs(liveTextPath, "", cfg.Video, fontSize, textColor, liveTextYExpr(cfg.Video.TextPosition), "")
	draw = draw + ":reload=1"
	filter := fmt.Sprintf("[0:v]scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,%s[v]", width, height, width, height, draw)

	chunkPattern := filepath.Join(chunksDir, "chunk_%05d.wav")
	args = append(args, "-filter_complex", filter)
	if opts.Stream {
		args = append(args,
			"-map", "[v]",
			"-map", "1:a",
			"-r", "30",
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-tune", "zerolatency",
			"-crf", "23",
			"-g", "60",
			"-c:a", "aac",
			"-b:a", "160k",
			"-pix_fmt", "yuv420p",
		)
		if opts.Duration > 0 {
			args = append(args, "-t", fmt.Sprintf("%.3f", opts.Duration.Seconds()))
		}
		args = append(args, "-f", opts.StreamFormat, opts.StreamURL)
	} else {
		args = append(args,
			"-map", "[v]",
			"-map", "1:a",
			"-t", fmt.Sprintf("%.3f", opts.Duration.Seconds()),
			"-r", "30",
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "20",
			"-c:a", "aac",
			"-b:a", "192k",
			"-pix_fmt", "yuv420p",
			"-movflags", "+frag_keyframe+empty_moov",
			opts.Output,
		)
	}
	args = append(args, "-map", "1:a")
	if opts.Duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", opts.Duration.Seconds()))
	}
	args = append(args,
		"-c:a", "pcm_s16le",
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%.2f", opts.ChunkSec),
		"-reset_timestamps", "1",
		chunkPattern,
	)

	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(verses) > 0 {
			errCh <- liveTextWorker(workerCtx, aligner, verses, opts.Mode, chunksDir, liveTextPath, cfg.Audio, logger)
			return
		}
		errCh <- liveAutoTextWorker(workerCtx, aligner, cfg.QuranAPI, opts.ExpectedSurah, opts.Mode, chunksDir, liveTextPath, cfg.Audio, logger)
	}()

	captureCtx := context.Background()
	captureCancel := func() {}
	if opts.Duration > 0 {
		// Live encoding can run substantially slower than realtime while Whisper is processing chunks.
		captureCtx, captureCancel = context.WithTimeout(captureCtx, opts.Duration*2)
	}
	defer captureCancel()
	cmd := exec.CommandContext(captureCtx, "ffmpeg", args...)
	// SIGINT lets FFmpeg finish the MP4 trailer when a live source ignores its output duration.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if len(verses) > 0 {
		logger.Infof("Starting live capture: source=%s surah %d ayahs %d-%d mode=%s", sourceLabel, opts.Surah, opts.StartAyah, opts.EndAyah, opts.Mode)
	} else {
		logger.Infof("Starting live capture: source=%s stream-detect expected-surah=%d mode=%s", sourceLabel, opts.ExpectedSurah, opts.Mode)
	}
	if opts.Stream {
		logger.Infof("Live stream output: %s (%s)", opts.StreamURL, opts.StreamFormat)
	}
	if err := cmd.Run(); err != nil && captureCtx.Err() != context.DeadlineExceeded {
		cancel()
		wg.Wait()
		return fmt.Errorf("live capture failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	cancel()
	wg.Wait()
	if workerErr := <-errCh; workerErr != nil {
		return workerErr
	}
	if opts.Stream {
		logger.Infof("Live stream ended")
	} else {
		logger.Infof("Live video generated: %s", opts.Output)
	}
	return nil
}

func liveMicInputArgs(device string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		in := strings.TrimSpace(device)
		if in == "" {
			in = ":0"
		}
		return []string{"-f", "avfoundation", "-i", in}, nil
	case "linux":
		in := strings.TrimSpace(device)
		if in == "" {
			in = "default"
		}
		return []string{"-f", "pulse", "-i", in}, nil
	default:
		return nil, fmt.Errorf("live mic mode is not supported on %s", runtime.GOOS)
	}
}

func resolveLiveAudioInputArgs(ctx context.Context, opts liveOptions) ([]string, string, error) {
	if strings.TrimSpace(opts.AudioURL) != "" {
		args, err := liveStreamInputArgs(opts.AudioURL, opts.Duration)
		return args, "audio-url", err
	}
	if strings.TrimSpace(opts.YouTubeURL) != "" {
		streamURL, err := background.ResolveYouTubeAudioStreamURL(ctx, opts.YouTubeURL, opts.YTDLPCmd)
		if err != nil {
			return nil, "", err
		}
		args, err := liveStreamInputArgs(streamURL, opts.Duration)
		return args, "youtube", err
	}
	args, err := liveMicInputArgs(opts.MicDevice)
	return args, "mic", err
}

func liveStreamInputArgs(streamURL string, duration time.Duration) ([]string, error) {
	u := strings.TrimSpace(streamURL)
	if u == "" {
		return nil, fmt.Errorf("empty stream URL")
	}
	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "2",
	}
	if duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", duration.Seconds()))
	}
	return append(args, "-i", u), nil
}

func liveTextYExpr(position string) string {
	switch strings.ToLower(strings.TrimSpace(position)) {
	case "lower-third", "lower", "bottom":
		return "(h*0.65)-(text_h/2)"
	case "upper", "top":
		return "(h*0.25)-(text_h/2)"
	default:
		return "(h-text_h)/2"
	}
}

func liveTextWorker(ctx context.Context, aligner *align.WhisperAligner, verses []quran.Verse, mode string, chunksDir, textPath string, audioCfg config.AudioConfig, logger *utils.Logger) error {
	ayahs := make([]ayahTokens, 0, len(verses))
	for _, v := range verses {
		words := strings.Fields(v.Text)
		norm := make([]string, 0, len(words))
		for _, w := range words {
			norm = append(norm, align.NormalizeWord(w))
		}
		ayahs = append(ayahs, ayahTokens{verse: v, words: words, norm: norm})
	}
	processed := map[string]bool{}
	currentAyah := 0
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		finalPass := false
		select {
		case <-ctx.Done():
			finalPass = true
		case <-ticker.C:
		}
		files := liveChunkFiles(chunksDir)
		sort.Strings(files)
		for _, f := range files {
			if processed[f] {
				continue
			}
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			if !finalPass && time.Since(info.ModTime()) < 300*time.Millisecond {
				continue
			}
			chunkPath := prepareChunkForWhisper(ctx, f, audioCfg)
			words, err := aligner.TranscribeWordsContext(ctx, chunkPath, audioCfg.Language)
			if err != nil {
				if finalPass {
					processed[f] = true
				}
				continue
			}
			processed[f] = true
			tokens := normalizeSegmentTokens(words)
			if len(tokens) == 0 {
				continue
			}
			bestIdx, bestMatch := selectBestAyahMatch(tokens, ayahs, currentAyah)
			if bestIdx < 0 {
				continue
			}
			if bestIdx > currentAyah {
				currentAyah = bestIdx
			}
			text := ayahs[bestIdx].verse.Text
			if mode == "2-word" {
				text = liveTwoWordText(ayahs[bestIdx], bestMatch)
				if strings.TrimSpace(text) == "" {
					text = ayahs[bestIdx].verse.Text
				}
			}
			if err := writeLiveTextAtomic(textPath, text); err != nil {
				logger.Warnf("Failed to update live text: %v", err)
			}
		}
		if finalPass {
			return nil
		}
	}
}

type cachedCorpus struct {
	base recognize.QuranCorpus
	mu   sync.Mutex
	data map[int][]recognize.Ayah
}

func (c *cachedCorpus) FetchSurah(ctx context.Context, surah int) ([]recognize.Ayah, error) {
	c.mu.Lock()
	if c.data == nil {
		c.data = make(map[int][]recognize.Ayah)
	}
	if ayahs, ok := c.data[surah]; ok {
		cached := make([]recognize.Ayah, len(ayahs))
		copy(cached, ayahs)
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()
	ayahs, err := c.base.FetchSurah(ctx, surah)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.data[surah] = append([]recognize.Ayah(nil), ayahs...)
	c.mu.Unlock()
	copied := make([]recognize.Ayah, len(ayahs))
	copy(copied, ayahs)
	return copied, nil
}

func liveAutoTextWorker(
	ctx context.Context,
	aligner *align.WhisperAligner,
	quranCfg config.QuranAPIConfig,
	expectedSurah int,
	mode string,
	chunksDir, textPath string,
	audioCfg config.AudioConfig,
	logger *utils.Logger,
) error {
	corpus := &cachedCorpus{base: newRecognizeCorpus(quranCfg)}
	processed := map[string]bool{}
	var (
		currentSurah int
		currentAyahs []ayahTokens
		currentIdx   int
		recentTokens []string
	)
	ticker := time.NewTicker(450 * time.Millisecond)
	defer ticker.Stop()
	for {
		finalPass := false
		select {
		case <-ctx.Done():
			finalPass = true
		case <-ticker.C:
		}

		files := liveChunkFiles(chunksDir)
		sort.Strings(files)
		for _, f := range files {
			if processed[f] {
				continue
			}
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			if !finalPass && time.Since(info.ModTime()) < 300*time.Millisecond {
				continue
			}
			chunkPath := prepareChunkForWhisper(ctx, f, audioCfg)
			words, err := aligner.TranscribeWordsContext(ctx, chunkPath, audioCfg.Language)
			if err != nil {
				if finalPass {
					processed[f] = true
				}
				continue
			}
			processed[f] = true
			if len(words) == 0 {
				continue
			}
			tokens := normalizeSegmentTokens(words)
			if len(tokens) == 0 {
				continue
			}
			recentTokens = append(recentTokens, tokens...)
			if len(recentTokens) > 48 {
				recentTokens = recentTokens[len(recentTokens)-48:]
			}

			if len(currentAyahs) > 0 {
				if bestIdx, bestMatch := selectBestAyahMatch(tokens, currentAyahs, currentIdx); bestIdx >= 0 {
					currentIdx = bestIdx
					text := currentAyahs[bestIdx].verse.Text
					if mode == "2-word" {
						text = liveTwoWordText(currentAyahs[bestIdx], bestMatch)
						if strings.TrimSpace(text) == "" {
							text = currentAyahs[bestIdx].verse.Text
						}
					}
					if err := writeLiveTextAtomic(textPath, text); err != nil {
						logger.Warnf("Failed to update live text: %v", err)
					}
					continue
				}
			}

			if !shouldAttemptStreamIdentify(tokens, recentTokens) {
				continue
			}
			segmentText := strings.TrimSpace(strings.Join(recentTokens, " "))
			result, ok := identifyChunkAyah(ctx, corpus, segmentText, currentSurah, expectedSurah)
			if !ok {
				continue
			}
			ayahs, err := corpus.FetchSurah(ctx, result.Surah)
			if err != nil || len(ayahs) == 0 {
				continue
			}
			start := result.StartAyah - 1
			if start < 1 {
				start = 1
			}
			end := result.EndAyah + 1
			if end > len(ayahs) {
				end = len(ayahs)
			}
			currentAyahs = ayahTokensFromRecognizeAyahs(result.Surah, ayahs, start, end)
			currentSurah = result.Surah
			currentIdx = 0
			if len(currentAyahs) == 0 {
				continue
			}
			bestIdx, bestMatch := selectBestAyahMatch(tokens, currentAyahs, currentIdx)
			if bestIdx >= 0 {
				currentIdx = bestIdx
			} else {
				bestIdx = 0
			}
			text := currentAyahs[bestIdx].verse.Text
			if mode == "2-word" && bestIdx >= 0 {
				text = liveTwoWordText(currentAyahs[bestIdx], bestMatch)
				if strings.TrimSpace(text) == "" {
					text = currentAyahs[bestIdx].verse.Text
				}
			}
			if err := writeLiveTextAtomic(textPath, text); err != nil {
				logger.Warnf("Failed to update live text: %v", err)
			}
			recentTokens = recentTokens[:0]
		}
		if finalPass {
			return nil
		}
	}
}

func shouldAttemptStreamIdentify(tokens []string, recentTokens []string) bool {
	if len(tokens) < 4 {
		return false
	}
	if len(recentTokens) < 10 {
		return false
	}
	if hasNonQuranPromoTokens(tokens) {
		return false
	}
	return true
}

func prepareChunkForWhisper(ctx context.Context, chunkPath string, cfg config.AudioConfig) string {
	if !cfg.EchoReduction {
		return chunkPath
	}
	cleanPath := chunkPath + ".clean.wav"
	if err := audio.EnhanceForSpeechRecognition(ctx, chunkPath, cleanPath, cfg.EchoFilter); err != nil {
		return chunkPath
	}
	return cleanPath
}

func liveChunkFiles(chunksDir string) []string {
	files, _ := filepath.Glob(filepath.Join(chunksDir, "chunk_*.wav"))
	filtered := files[:0]
	for _, file := range files {
		if strings.Contains(filepath.Base(file), ".clean.wav") {
			continue
		}
		filtered = append(filtered, file)
	}
	return filtered
}

func hasNonQuranPromoTokens(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	bad := map[string]struct{}{
		"اشتركوا":  {},
		"اشترك":    {},
		"قناة":     {},
		"القناة":   {},
		"ترجمة":    {},
		"شكرا":     {},
		"سبسكرايب": {},
		"متابعة":   {},
	}
	hits := 0
	for _, t := range tokens {
		if _, ok := bad[t]; ok {
			hits++
		}
	}
	return hits >= 2
}

func identifyChunkAyah(ctx context.Context, corpus recognize.QuranCorpus, segmentText string, currentSurah, expectedSurah int) (recognize.Result, bool) {
	try := func(expected int) (recognize.Result, bool) {
		m := recognize.Matcher{Corpus: corpus, ExpectedSurah: expected}
		r, err := m.Identify(ctx, segmentText)
		if err != nil {
			return recognize.Result{}, false
		}
		return r, true
	}
	if currentSurah >= 1 && currentSurah <= 114 {
		if r, ok := try(currentSurah); ok {
			return r, true
		}
	}
	if expectedSurah >= 1 && expectedSurah <= 114 && expectedSurah != currentSurah {
		if r, ok := try(expectedSurah); ok {
			return r, true
		}
	}
	r, ok := try(0)
	return r, ok
}

func ayahTokensFromRecognizeAyahs(surah int, ayahs []recognize.Ayah, startAyah, endAyah int) []ayahTokens {
	if startAyah < 1 {
		startAyah = 1
	}
	if endAyah > len(ayahs) {
		endAyah = len(ayahs)
	}
	if endAyah < startAyah {
		return nil
	}
	out := make([]ayahTokens, 0, endAyah-startAyah+1)
	for i := startAyah; i <= endAyah; i++ {
		a := ayahs[i-1]
		text := strings.TrimSpace(a.Text)
		words := strings.Fields(text)
		norm := make([]string, 0, len(words))
		for _, w := range words {
			norm = append(norm, align.NormalizeWord(w))
		}
		out = append(out, ayahTokens{
			verse: quran.Verse{
				NumberInSurah: a.NumberInSurah,
				Text:          text,
				SurahMeta: quran.SurahMeta{
					Number: surah,
				},
			},
			words: words,
			norm:  norm,
		})
	}
	return out
}

func liveTwoWordText(ayah ayahTokens, match ayahMatch) string {
	words := ayah.words
	if len(words) == 0 {
		return ""
	}
	idx := match.end
	if idx < 0 {
		idx = 0
	}
	if idx >= len(words) {
		idx = len(words) - 1
	}
	start := (idx / 2) * 2
	end := start + 2
	if end > len(words) {
		end = len(words)
	}
	if start < 0 || start >= len(words) || end <= start {
		return ""
	}
	return strings.Join(words[start:end], " ")
}

func writeLiveTextAtomic(path, text string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
