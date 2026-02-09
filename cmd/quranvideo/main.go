package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"qgencodex/internal/ai"
	"qgencodex/internal/align"
	"qgencodex/internal/audio"
	"qgencodex/internal/background"
	"qgencodex/internal/batch"
	"qgencodex/internal/caption"
	"qgencodex/internal/config"
	"qgencodex/internal/ffmpeg"
	"qgencodex/internal/quran"
	"qgencodex/internal/recognize"
	"qgencodex/internal/render"
	"qgencodex/internal/utils"
)

const appName = "quranvideo"

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]
	switch cmd {
	case "generate":
		generateCmd(os.Args[2:])
	case "generate-audio":
		generateAudioCmd(os.Args[2:])
	case "identify":
		identifyCmd(os.Args[2:])
	case "batch":
		batchCmd(os.Args[2:])
	case "config":
		configCmd(os.Args[2:])
	case "version":
		fmt.Println("quranvideo v0.1.0")
	default:
		usage()
	}
}

func usage() {
	fmt.Println(`Quran Video CLI

Usage:
  quranvideo generate [options]
  quranvideo generate-audio --audio recitation.mp3
  quranvideo identify --audio recitation.mp3
  quranvideo batch --file batch.yaml
  quranvideo config init
  quranvideo version

Run 'quranvideo generate -h' for generate options.`)
}

func generateAudioCmd(args []string) {
	fs := flag.NewFlagSet("generate-audio", flag.ExitOnError)
	audioPath := fs.String("audio", "", "Recitation audio file")
	expectedSurah := fs.Int("expected-surah", 0, "Optional expected surah number (1-114)")
	surah := fs.Int("surah", 0, "Optional surah number (1-114)")
	startAyah := fs.Int("start", 0, "Optional start ayah")
	endAyah := fs.Int("end", 0, "Optional end ayah")
	mode := fs.String("mode", "sequential", "Display mode: sequential|word-by-word")
	output := fs.String("output", "", "Output video path")
	configPath := fs.String("config", "", "Config file path")
	translation := fs.Bool("translation", true, "Include translation overlay")
	backgroundPath := fs.String("background", "", "Custom background video path")
	noBackground := fs.Bool("no-background", false, "Disable background video (solid color)")
	_ = fs.Parse(args)

	if *audioPath == "" {
		exitWithError(fmt.Errorf("audio path is required"))
	}

	cfg, created, err := loadConfig(*configPath)
	if err != nil {
		exitWithError(err)
	}
	logger := utils.NewLogger(cfg.Logging.Level)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(*configPath))
	}

	ctx := context.Background()
	var result recognize.Result
	if *surah > 0 && *startAyah > 0 && *endAyah > 0 {
		result = recognize.Result{Surah: *surah, StartAyah: *startAyah, EndAyah: *endAyah}
		logger.Infof("Using provided recitation range: Surah %d, Ayahs %d-%d", result.Surah, result.StartAyah, result.EndAyah)
	} else {
		recognizer := recognize.NewWhisperRecognizer(cfg.Audio.WhisperCmd)
		if !recognizer.Available() {
			exitWithError(fmt.Errorf("whisper not available"))
		}
		matcher := recognize.Matcher{
			Corpus:        &recognize.APICorpus{BaseURL: cfg.QuranAPI.BaseURL, Edition: cfg.QuranAPI.Edition, Timeout: time.Duration(cfg.QuranAPI.TimeoutSec) * time.Second},
			ExpectedSurah: *expectedSurah,
		}
		detected, transcript, err := recognizer.Identify(ctx, *audioPath, cfg.Audio.Language, &matcher)
		if err != nil {
			logger.Warnf("Identify failed: %v", err)
			if transcript != "" {
				logger.Infof("Transcript: %s", transcript)
			}
			exitWithError(err)
		}
		result = detected
		logger.Infof("Detected recitation: Surah %d, Ayahs %d-%d", result.Surah, result.StartAyah, result.EndAyah)
	}

	opts := generateOptions{
		Surah:              result.Surah,
		StartAyah:          result.StartAyah,
		EndAyah:            result.EndAyah,
		Mode:               *mode,
		Output:             *output,
		ConfigPath:         *configPath,
		IncludeTranslation: *translation,
		BackgroundPath:     *backgroundPath,
		NoBackground:       *noBackground,
		AudioPath:          *audioPath,
	}
	if err := runGenerate(opts); err != nil {
		exitWithError(err)
	}
}

func identifyCmd(args []string) {
	fs := flag.NewFlagSet("identify", flag.ExitOnError)
	audioPath := fs.String("audio", "", "Recitation audio file")
	expectedSurah := fs.Int("expected-surah", 0, "Optional expected surah number (1-114)")
	configPath := fs.String("config", "", "Config file path")
	_ = fs.Parse(args)
	if *audioPath == "" {
		exitWithError(fmt.Errorf("audio path is required"))
	}
	cfg, created, err := loadConfig(*configPath)
	if err != nil {
		exitWithError(err)
	}
	logger := utils.NewLogger(cfg.Logging.Level)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(*configPath))
	}
	recognizer := recognize.NewWhisperRecognizer(cfg.Audio.WhisperCmd)
	if !recognizer.Available() {
		exitWithError(fmt.Errorf("whisper not available"))
	}
	ctx := context.Background()
	matcher := recognize.Matcher{
		Corpus:        &recognize.APICorpus{BaseURL: cfg.QuranAPI.BaseURL, Edition: cfg.QuranAPI.Edition, Timeout: time.Duration(cfg.QuranAPI.TimeoutSec) * time.Second},
		ExpectedSurah: *expectedSurah,
	}
	result, transcript, err := recognizer.Identify(ctx, *audioPath, cfg.Audio.Language, &matcher)
	if err != nil {
		logger.Warnf("Identify failed: %v", err)
		if transcript != "" {
			logger.Infof("Transcript: %s", transcript)
		}
		exitWithError(err)
	}
	fmt.Printf("Surah %d, Ayahs %d-%d\n", result.Surah, result.StartAyah, result.EndAyah)
}

type generateOptions struct {
	Surah              int
	StartAyah          int
	EndAyah            int
	Mode               string
	Output             string
	ConfigPath         string
	IncludeTranslation bool
	BackgroundPath     string
	NoBackground       bool
	AudioPath          string
}

func generateCmd(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	opts := generateOptions{}
	fs.IntVar(&opts.Surah, "surah", 1, "Surah number (1-114)")
	fs.IntVar(&opts.StartAyah, "start", 1, "Start ayah in surah")
	fs.IntVar(&opts.EndAyah, "end", 1, "End ayah in surah")
	fs.StringVar(&opts.Mode, "mode", "sequential", "Display mode: sequential|word-by-word")
	fs.StringVar(&opts.Output, "output", "", "Output video path")
	fs.StringVar(&opts.ConfigPath, "config", "", "Config file path")
	fs.BoolVar(&opts.IncludeTranslation, "translation", true, "Include translation overlay")
	fs.StringVar(&opts.BackgroundPath, "background", "", "Custom background video path")
	fs.BoolVar(&opts.NoBackground, "no-background", false, "Disable background video (solid color)")
	_ = fs.Parse(args)

	if err := runGenerate(opts); err != nil {
		exitWithError(err)
	}
}

func runGenerate(opts generateOptions) error {
	cfg, created, err := loadConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	logger := utils.NewLogger(cfg.Logging.Level)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(opts.ConfigPath))
	}
	var aiClient *ai.Client
	if cfg.AI.Enabled {
		aiClient = &ai.Client{
			BaseURL: cfg.AI.BaseURL,
			Model:   cfg.AI.Model,
			Timeout: time.Duration(cfg.AI.TimeoutSec) * time.Second,
		}
	}
	if cfg.Video.Font.File == "" && cfg.Video.Font.Family != "" {
		if resolved := render.ResolveFontFile(cfg.Video.Font.Family); resolved != "" {
			cfg.Video.Font.File = resolved
			logger.Debugf("Resolved font file: %s", resolved)
		} else {
			logger.Warnf("Font family %q not found on system; consider setting video.font.file to a .ttf/.otf path", cfg.Video.Font.Family)
		}
	}

	if opts.Output == "" {
		outputName := fmt.Sprintf("surah%d_%d-%d_%s.mp4", opts.Surah, opts.StartAyah, opts.EndAyah, strings.ReplaceAll(opts.Mode, " ", "-"))
		opts.Output = filepath.Join(cfg.Output.Dir, outputName)
	}

	ctx := context.Background()
	logger.Infof("Fetching verses: Surah %d, ayahs %d-%d", opts.Surah, opts.StartAyah, opts.EndAyah)
	client := quran.NewClient(cfg.QuranAPI.BaseURL, time.Duration(cfg.QuranAPI.TimeoutSec)*time.Second)
	verses, err := client.FetchVerses(ctx, opts.Surah, opts.StartAyah, opts.EndAyah, cfg.QuranAPI.Edition, cfg.QuranAPI.Translation)
	if err != nil {
		return err
	}

	ayahNumbers := make([]int, len(verses))
	for i, v := range verses {
		ayahNumbers[i] = v.Number
	}

	tempDir := cfg.Output.TempDir
	if err := utils.EnsureDir(tempDir); err != nil {
		return err
	}
	var (
		segments      []audio.Segment
		audioPath     string
		audioDuration time.Duration
	)
	if opts.AudioPath != "" {
		if !utils.FileExists(opts.AudioPath) {
			return fmt.Errorf("audio file not found: %s", opts.AudioPath)
		}
		audioPath = opts.AudioPath
		logger.Infof("Using recitation audio: %s", audioPath)
		if cfg.Audio.TrimSilence {
			trimmed := filepath.Join(tempDir, "recitation_trim.mp3")
			if err := audio.TrimSilence(ctx, audioPath, trimmed, cfg.Audio.BitrateKbps, cfg.Audio.SilenceDB, cfg.Audio.SilenceSec); err == nil {
				audioPath = trimmed
			} else {
				logger.Warnf("Failed to trim silence: %v", err)
			}
		}
		durSec, err := ffmpeg.ProbeDuration(ctx, audioPath)
		if err != nil {
			return err
		}
		audioDuration = time.Duration(durSec * float64(time.Second))
		segments = buildSegmentsFromDuration(verses, audioDuration)
	} else {
		audioDir := filepath.Join(tempDir, "audio")
		if err := utils.EnsureDir(audioDir); err != nil {
			return err
		}
		logger.Infof("Downloading audio segments for %d ayahs", len(ayahNumbers))
		ad := audio.Downloader{
			BaseURL:       cfg.Audio.CDNBaseURL,
			Reciter:       cfg.QuranAPI.Reciter,
			BitrateKbps:   cfg.Audio.BitrateKbps,
			Timeout:       time.Duration(cfg.QuranAPI.TimeoutSec) * time.Second,
			MaxConcurrent: cfg.Audio.MaxConcurrent,
			RemoveSilence: cfg.Audio.TrimSilence,
			SilenceDB:     cfg.Audio.SilenceDB,
			SilenceSec:    cfg.Audio.SilenceSec,
		}
		segments, err = ad.DownloadSegments(ctx, ayahNumbers, audioDir)
		if err != nil {
			return err
		}

		audioPath = filepath.Join(tempDir, "audio_concat.mp3")
		logger.Infof("Concatenating audio segments")
		if err := audio.Concat(ctx, segments, audioPath, tempDir); err != nil {
			return err
		}
		for _, seg := range segments {
			audioDuration += seg.Duration
		}
	}

	logger.Infof("Preparing timings")
	timings, err := render.BuildTimings(verses, segments)
	if err != nil {
		return err
	}
	mode := strings.ToLower(opts.Mode)
	if mode == "word-by-word" || mode == "word" || mode == "two-by-two" || mode == "two" || mode == "pair" || mode == "2x2" {
		if opts.AudioPath != "" {
			_ = applyWordAlignmentFullAudio(ctx, timings, audioPath, cfg.Audio, logger)
		} else {
			_ = applyWordAlignment(ctx, timings, segments, audioPath, cfg.Audio, logger)
		}
	}
	if opts.AudioPath != "" && strings.EqualFold(opts.Mode, "sequential") {
		if applyWordAlignmentFullAudio(ctx, timings, audioPath, cfg.Audio, logger) {
			if applyAyahBoundariesFromWordTimings(timings) {
				logger.Infof("Aligned ayah boundaries to recitation audio")
			}
		}
	}
	if strings.EqualFold(opts.Mode, "sequential") && cfg.Audio.PauseSensitive {
		ensureWordTimings(ctx, opts.AudioPath != "", timings, segments, audioPath, cfg.Audio, logger)
		silences, err := audio.DetectSilences(ctx, audioPath, cfg.Audio.PauseDB, cfg.Audio.PauseSec)
		if err != nil {
			logger.Warnf("Pause-sensitive display failed: %v", err)
		} else if len(silences) > 0 {
			timings = splitTimingsOnSilence(timings, silences, 120*time.Millisecond)
		}
	} else if strings.EqualFold(opts.Mode, "sequential") {
		ensureContinuousTimings(timings, audioDuration)
	}

	bgPath := ""
	if opts.BackgroundPath != "" {
		totalDuration := timings[len(timings)-1].End
		resolved, err := resolveBackgroundInput(ctx, opts.BackgroundPath, tempDir, time.Duration(cfg.QuranAPI.TimeoutSec)*time.Second, logger, totalDuration)
		if err != nil {
			return err
		}
		bgPath = resolved
		logger.Infof("Using custom background: %s", bgPath)
	} else if !opts.NoBackground {
		bgTimeoutSec := cfg.Background.TimeoutSec
		if bgTimeoutSec <= 0 {
			bgTimeoutSec = cfg.QuranAPI.TimeoutSec
		}
		client := backgroundClient(cfg.Background, time.Duration(bgTimeoutSec)*time.Second)
		if client != nil {
			selector := &background.Selector{
				Client:           client,
				FallbackQuery:    cfg.Background.QueryFallback,
				Orientation:      cfg.Background.Orientation,
				MinDuration:      cfg.Background.MinDurationSec,
				Timeout:          time.Duration(bgTimeoutSec) * time.Second,
				Quality:          cfg.Background.Quality,
				MaxWidth:         cfg.Background.MaxWidth,
				MaxHeight:        cfg.Background.MaxHeight,
				MaxPixels:        cfg.Background.MaxPixels,
				UseContext:       cfg.Background.UseContext,
				Random:           cfg.Background.Random,
				UseAI:            cfg.Background.UseAI && aiClient != nil,
				AIClient:         aiClient,
				AISelect:         cfg.Background.AISelect && aiClient != nil,
				ExcludePeople:    cfg.Background.ExcludePeople,
				ExcludeReligious: cfg.Background.ExcludeReligious,
			}
			longMin := cfg.Background.LongMinDurationSec
			if longMin <= 0 {
				longMin = 30
			}
			longThreshold := cfg.Background.LongThresholdSec
			if longThreshold <= 0 {
				longThreshold = 25
			}
			if maxTimingDuration(timings) >= time.Duration(longThreshold)*time.Second {
				if selector.MinDuration < longMin {
					selector.MinDuration = longMin
				}
			}

			if cfg.Background.PerAyah && strings.EqualFold(opts.Mode, "sequential") {
				width, height, err := render.ParseResolution(cfg.Video.Resolution)
				if err != nil {
					logger.Warnf("Invalid resolution for per-ayah background: %v; falling back to single background", err)
				} else {
					logger.Infof("Building per-ayah backgrounds")
					bgPath = filepath.Join(tempDir, "background_sequence.mp4")
					segments := make([]background.Segment, 0, len(timings))
					for _, t := range timings {
						minDur := cfg.Background.MinDurationSec
						if t.End-t.Start >= time.Duration(longThreshold)*time.Second {
							if minDur < longMin {
								minDur = longMin
							}
						}
						segments = append(segments, background.Segment{
							Text:           t.Verse.Text,
							Duration:       t.End - t.Start,
							MinDurationSec: minDur,
						})
					}
					if err := background.BuildSequence(ctx, selector, segments, width, height, tempDir, bgPath); err != nil {
						logger.Warnf("Per-ayah background failed: %v; falling back to single background", err)
						bgPath = ""
					}
				}
			}

			if bgPath == "" {
				bgDir := filepath.Join(tempDir, "background")
				_ = utils.EnsureDir(bgDir)
				bgPath = filepath.Join(bgDir, "background.mp4")
				logger.Infof("Selecting background video")
				var selection background.Selection
				var err error
				texts := make([]string, 0, len(verses))
				for _, v := range verses {
					texts = append(texts, v.Text)
				}
				selection, err = selector.SelectFromPool(ctx, texts, bgPath)
				if selection.VideoURL != "" {
					logger.Debugf("Background selection: query=%q url=%s size=%dx%d duration=%ds", selection.Query, selection.VideoURL, selection.Width, selection.Height, selection.Duration)
				}
				if err != nil {
					if selection.VideoURL != "" {
						logger.Warnf("Background download failed for url=%s: %v; falling back to solid color", selection.VideoURL, err)
					} else {
						logger.Warnf("Background selection failed: %v; falling back to solid color", err)
					}
					bgPath = ""
				}
			}
		} else {
			logger.Infof("No background provider configured; using solid background")
		}
	}

	if cfg.Output.Captions {
		captionsPath := strings.TrimSuffix(opts.Output, filepath.Ext(opts.Output)) + ".srt"
		logger.Infof("Writing captions: %s", captionsPath)
		if err := caption.WriteSRT(captionsPath, timings, opts.IncludeTranslation); err != nil {
			logger.Warnf("Failed to write captions: %v", err)
		}
	}

	logger.Infof("Rendering video")
	err = render.Render(ctx, render.RenderInput{
		Timings:            timings,
		AudioPath:          audioPath,
		BackgroundPath:     bgPath,
		OutputPath:         opts.Output,
		TempDir:            tempDir,
		Mode:               opts.Mode,
		VideoConfig:        cfg.Video,
		IncludeTranslation: opts.IncludeTranslation,
	})
	if err != nil {
		return err
	}
	logger.Infof("Video generated: %s", opts.Output)
	return nil
}

func batchCmd(args []string) {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	var (
		batchFile  = fs.String("file", "", "Batch YAML file")
		configPath = fs.String("config", "", "Config file path")
	)
	_ = fs.Parse(args)
	if *batchFile == "" {
		exitWithError(fmt.Errorf("batch file is required"))
	}
	cfg, created, err := loadConfig(*configPath)
	if err != nil {
		exitWithError(err)
	}
	logger := utils.NewLogger(cfg.Logging.Level)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(*configPath))
	}
	b, err := batch.Load(*batchFile)
	if err != nil {
		exitWithError(err)
	}
	if len(b.Jobs) == 0 {
		exitWithError(fmt.Errorf("no jobs found in batch file"))
	}
	for idx, job := range b.Jobs {
		logger.Infof("Starting batch job %d/%d", idx+1, len(b.Jobs))
		output := job.OutputName
		if output == "" {
			output = fmt.Sprintf("surah%d_%d-%d_%s.mp4", job.Surah, job.StartAyah, job.EndAyah, strings.ReplaceAll(job.Mode, " ", "-"))
		}
		err := runGenerate(generateOptions{
			Surah:              job.Surah,
			StartAyah:          job.StartAyah,
			EndAyah:            job.EndAyah,
			Mode:               job.Mode,
			Output:             filepath.Join(cfg.Output.Dir, output),
			ConfigPath:         resolveConfigPath(*configPath),
			IncludeTranslation: true,
		})
		if err != nil {
			logger.Warnf("Batch job %d failed: %v", idx+1, err)
			continue
		}
	}
}

func configCmd(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		fmt.Println("Usage: quranvideo config init")
		return
	}
	sub := fs.Arg(0)
	switch sub {
	case "init":
		path := resolveConfigPath("")
		cfg := config.Default()
		if err := config.Write(path, cfg); err != nil {
			exitWithError(err)
		}
		fmt.Printf("Default config written to %s\n", path)
	default:
		fmt.Println("Unknown config command")
	}
}

func loadConfig(path string) (*config.Config, bool, error) {
	resolved := resolveConfigPath(path)
	cfg, created, err := config.LoadOrCreate(resolved)
	if err != nil {
		return nil, false, err
	}
	return cfg, created, nil
}

func resolveConfigPath(path string) string {
	if path != "" {
		return path
	}
	defaultPath, err := config.DefaultConfigPath()
	if err != nil {
		return "config.yaml"
	}
	return defaultPath
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func applyWordAlignment(ctx context.Context, timings []render.Timing, segments []audio.Segment, audioPath string, cfg config.AudioConfig, logger *utils.Logger) bool {
	mode := strings.ToLower(cfg.WordTiming)
	if mode == "" {
		mode = "auto"
	}
	if mode == "even" {
		return false
	}
	aligner := align.NewWhisperAligner(cfg.WhisperCmd)
	if !aligner.Available() {
		if mode == "whisper" {
			logger.Warnf("Whisper not available; falling back to even word timing")
		}
		return false
	}
	alignedAny := false
	for i := range timings {
		if i >= len(segments) {
			break
		}
		words := strings.Fields(timings[i].Verse.Text)
		if len(words) == 0 {
			continue
		}
		wordTimings, err := aligner.Align(segments[i].Path, words, cfg.Language)
		if err != nil {
			logger.Warnf("Word alignment failed for ayah %d: %v; using even split", timings[i].Verse.NumberInSurah, err)
			continue
		}
		mapped := make([]render.WordTiming, 0, len(wordTimings))
		for _, wt := range wordTimings {
			mapped = append(mapped, render.WordTiming{
				Word:  wt.Word,
				Start: timings[i].Start + wt.Start,
				End:   timings[i].Start + wt.End,
			})
		}
		timings[i].WordTimings = mapped
		if len(mapped) > 0 {
			alignedAny = true
		}
	}
	applyWordOffset(timings, computeWordOffset(ctx, audioPath, timings, cfg, logger))
	return alignedAny
}

func applyWordAlignmentFullAudio(ctx context.Context, timings []render.Timing, audioPath string, cfg config.AudioConfig, logger *utils.Logger) bool {
	mode := strings.ToLower(cfg.WordTiming)
	if mode == "" {
		mode = "auto"
	}
	if mode == "even" {
		return false
	}
	aligner := align.NewWhisperAligner(cfg.WhisperCmd)
	if !aligner.Available() {
		if mode == "whisper" {
			logger.Warnf("Whisper not available; falling back to even word timing")
		}
		return false
	}
	words := make([]string, 0, 512)
	verseIndex := make([]int, 0, 512)
	for i, t := range timings {
		ws := strings.Fields(t.Verse.Text)
		for _, w := range ws {
			words = append(words, w)
			verseIndex = append(verseIndex, i)
		}
	}
	if len(words) == 0 {
		return false
	}
	wordTimings, err := aligner.Align(audioPath, words, cfg.Language)
	if err != nil {
		logger.Warnf("Full-audio word alignment failed: %v; using even split", err)
		return false
	}
	perVerse := make([][]render.WordTiming, len(timings))
	for i, wt := range wordTimings {
		if i >= len(verseIndex) {
			break
		}
		vi := verseIndex[i]
		perVerse[vi] = append(perVerse[vi], render.WordTiming{Word: wt.Word, Start: wt.Start, End: wt.End})
	}
	for i := range timings {
		if len(perVerse[i]) > 0 {
			timings[i].WordTimings = perVerse[i]
		}
	}
	applyWordOffset(timings, computeWordOffset(ctx, audioPath, timings, cfg, logger))
	return true
}

func ensureWordTimings(ctx context.Context, useFullAudio bool, timings []render.Timing, segments []audio.Segment, audioPath string, cfg config.AudioConfig, logger *utils.Logger) {
	mode := strings.ToLower(cfg.WordTiming)
	if mode == "even" {
		return
	}
	if hasWordTimings(timings) {
		return
	}
	if useFullAudio {
		_ = applyWordAlignmentFullAudio(ctx, timings, audioPath, cfg, logger)
		return
	}
	_ = applyWordAlignment(ctx, timings, segments, audioPath, cfg, logger)
}

func hasWordTimings(timings []render.Timing) bool {
	for _, t := range timings {
		if len(t.WordTimings) > 0 {
			return true
		}
	}
	return false
}

func buildSegmentsFromDuration(verses []quran.Verse, total time.Duration) []audio.Segment {
	if len(verses) == 0 {
		return nil
	}
	weights := make([]int, len(verses))
	totalWeight := 0
	for i, v := range verses {
		words := strings.Fields(v.Text)
		weight := len(words)
		if weight == 0 {
			weight = len([]rune(v.Text)) / 3
		}
		if weight == 0 {
			weight = 1
		}
		weights[i] = weight
		totalWeight += weight
	}
	segments := make([]audio.Segment, len(verses))
	remaining := total
	for i := range verses {
		portion := total / time.Duration(totalWeight)
		dur := portion * time.Duration(weights[i])
		if i == len(verses)-1 {
			dur = remaining
		}
		segments[i] = audio.Segment{Duration: dur}
		remaining -= dur
	}
	return segments
}

func applyWordOffset(timings []render.Timing, offset time.Duration) {
	if offset == 0 {
		return
	}
	for i := range timings {
		verseStart := timings[i].Start
		verseEnd := timings[i].End
		for j := range timings[i].WordTimings {
			start := timings[i].WordTimings[j].Start + offset
			end := timings[i].WordTimings[j].End + offset
			if start < verseStart {
				start = verseStart
			}
			if end < start {
				end = start
			}
			if end > verseEnd {
				end = verseEnd
				if end < start {
					start = end
				}
			}
			timings[i].WordTimings[j].Start = start
			timings[i].WordTimings[j].End = end
		}
	}
}

func computeWordOffset(ctx context.Context, audioPath string, timings []render.Timing, cfg config.AudioConfig, logger *utils.Logger) time.Duration {
	offset := time.Duration(cfg.WordOffsetMs) * time.Millisecond
	if cfg.AutoWordOffset && audioPath != "" {
		starts := collectWordStarts(timings)
		if len(starts) >= 3 {
			window := cfg.AutoWordOffsetWindowMs
			if window <= 0 {
				window = 80
			}
			autoOffset, err := audio.EstimateWordOffset(ctx, audioPath, starts, window)
			if err != nil {
				logger.Warnf("Auto word offset failed: %v", err)
			} else {
				logger.Infof("Auto word offset: %dms", autoOffset.Milliseconds())
				offset += autoOffset
			}
		}
	}
	return offset
}

func collectWordStarts(timings []render.Timing) []time.Duration {
	starts := make([]time.Duration, 0, 256)
	for _, t := range timings {
		for _, wt := range t.WordTimings {
			if wt.End <= wt.Start {
				continue
			}
			starts = append(starts, wt.Start)
		}
	}
	return starts
}

func maxTimingDuration(timings []render.Timing) time.Duration {
	maxDur := time.Duration(0)
	for _, t := range timings {
		d := t.End - t.Start
		if d > maxDur {
			maxDur = d
		}
	}
	return maxDur
}

func ensureContinuousTimings(timings []render.Timing, total time.Duration) {
	if len(timings) == 0 {
		return
	}
	if timings[0].Start > 0 {
		timings[0].Start = 0
	}
	for i := 0; i < len(timings); i++ {
		if timings[i].End < timings[i].Start {
			timings[i].End = timings[i].Start
		}
		if i == len(timings)-1 {
			break
		}
		next := &timings[i+1]
		if next.Start < timings[i].End {
			next.Start = timings[i].End
		}
		if timings[i].End < next.Start {
			timings[i].End = next.Start
		}
	}
	if total > 0 && timings[len(timings)-1].End < total {
		timings[len(timings)-1].End = total
	}
}

func applyAyahBoundariesFromWordTimings(timings []render.Timing) bool {
	updated := false
	for i := range timings {
		wts := timings[i].WordTimings
		if len(wts) == 0 {
			continue
		}
		start := wts[0].Start
		end := wts[len(wts)-1].End
		if end < start {
			end = start
		}
		if start != timings[i].Start || end != timings[i].End {
			timings[i].Start = start
			timings[i].End = end
			updated = true
		}
	}
	if !updated {
		return false
	}
	for i := 0; i < len(timings); i++ {
		if timings[i].Start < 0 {
			timings[i].Start = 0
		}
		if timings[i].End < timings[i].Start {
			timings[i].End = timings[i].Start
		}
		if i == 0 {
			continue
		}
		prev := timings[i-1].End
		if timings[i].Start < prev {
			timings[i].Start = prev
		}
		if timings[i].End < timings[i].Start {
			timings[i].End = timings[i].Start
		}
	}
	return true
}

func backgroundClient(cfg config.BackgroundConfig, timeout time.Duration) background.VideoClient {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "pixabay":
		if strings.TrimSpace(cfg.PixabayAPIKey) != "" {
			return &background.PixabayClient{
				BaseURL: cfg.PixabayBaseURL,
				APIKey:  cfg.PixabayAPIKey,
				Timeout: timeout,
			}
		}
	case "pexels":
		if strings.TrimSpace(cfg.PexelsAPIKey) != "" {
			return &background.PexelsClient{
				BaseURL: cfg.PexelsBaseURL,
				APIKey:  cfg.PexelsAPIKey,
				Timeout: timeout,
			}
		}
	default:
		if strings.TrimSpace(cfg.PexelsAPIKey) != "" {
			return &background.PexelsClient{
				BaseURL: cfg.PexelsBaseURL,
				APIKey:  cfg.PexelsAPIKey,
				Timeout: timeout,
			}
		}
		if strings.TrimSpace(cfg.PixabayAPIKey) != "" {
			return &background.PixabayClient{
				BaseURL: cfg.PixabayBaseURL,
				APIKey:  cfg.PixabayAPIKey,
				Timeout: timeout,
			}
		}
	}
	return nil
}

func resolveBackgroundInput(ctx context.Context, inputPath, tempDir string, timeout time.Duration, logger *utils.Logger, duration time.Duration) (string, error) {
	if inputPath == "" {
		return "", fmt.Errorf("empty background path")
	}
	if isURL(inputPath) {
		if err := utils.EnsureDir(tempDir); err != nil {
			return "", err
		}
		if isYouTubeURL(inputPath) {
			dest := filepath.Join(tempDir, "background_youtube.mp4")
			logger.Infof("Downloading YouTube background")
			if duration > 0 {
				if err := background.DownloadYouTubeSegment(ctx, inputPath, dest, duration); err != nil {
					return "", err
				}
			} else if err := background.DownloadYouTube(ctx, inputPath, dest); err != nil {
				return "", err
			}
			return dest, nil
		}
		u, _ := url.Parse(inputPath)
		ext := filepath.Ext(u.Path)
		if ext == "" {
			ext = ".mp4"
		}
		dest := filepath.Join(tempDir, "background_url"+ext)
		if duration > 0 {
			logger.Infof("Downloading background URL segment")
			if err := background.DownloadURLSegment(ctx, inputPath, dest, duration); err != nil {
				return "", err
			}
		} else {
			client := utils.HTTPClient(timeout)
			logger.Infof("Downloading background URL")
			if err := utils.DownloadFile(ctx, client, inputPath, nil, dest); err != nil {
				return "", err
			}
		}
		return dest, nil
	}
	if !utils.FileExists(inputPath) {
		return "", fmt.Errorf("background file not found: %s", inputPath)
	}
	return inputPath, nil
}

func isURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

func isYouTubeURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return strings.Contains(host, "youtube.com") || strings.Contains(host, "youtu.be")
}

func splitTimingsOnSilence(timings []render.Timing, silences []audio.Silence, minSegment time.Duration) []render.Timing {
	if len(timings) == 0 || len(silences) == 0 {
		return timings
	}
	result := make([]render.Timing, 0, len(timings))
	for _, t := range timings {
		local := relevantSilences(t, silences)
		if len(local) == 0 {
			result = append(result, t)
			continue
		}
		if len(t.WordTimings) > 0 {
			if split := splitTimingByWordTimings(t, local, minSegment); len(split) > 0 {
				result = append(result, split...)
				continue
			}
		}
		segments := segmentsFromSilence(t, local)
		segments = filterSegments(segments, minSegment)
		if len(segments) == 0 {
			continue
		}
		result = append(result, splitTimingBySegments(t, segments)...)
	}
	if len(result) == 0 {
		return timings
	}
	return result
}

type segment struct {
	start time.Duration
	end   time.Duration
}

func relevantSilences(t render.Timing, silences []audio.Silence) []audio.Silence {
	var local []audio.Silence
	for _, s := range silences {
		if s.End <= t.Start || s.Start >= t.End {
			continue
		}
		local = append(local, s)
	}
	return local
}

func segmentsFromSilence(t render.Timing, silences []audio.Silence) []segment {
	segments := []segment{{start: t.Start, end: t.End}}
	for _, s := range silences {
		next := make([]segment, 0, len(segments))
		for _, seg := range segments {
			if s.End <= seg.start || s.Start >= seg.end {
				next = append(next, seg)
				continue
			}
			if s.Start <= seg.start && s.End >= seg.end {
				continue
			}
			if s.Start <= seg.start && s.End < seg.end {
				next = append(next, segment{start: s.End, end: seg.end})
				continue
			}
			if s.Start > seg.start && s.End >= seg.end {
				next = append(next, segment{start: seg.start, end: s.Start})
				continue
			}
			next = append(next, segment{start: seg.start, end: s.Start})
			next = append(next, segment{start: s.End, end: seg.end})
		}
		segments = next
		if len(segments) == 0 {
			break
		}
	}
	return segments
}

func segmentDurations(segments []segment) []time.Duration {
	durations := make([]time.Duration, 0, len(segments))
	for _, seg := range segments {
		durations = append(durations, seg.end-seg.start)
	}
	return durations
}

func filterSegments(segments []segment, minSegment time.Duration) []segment {
	if len(segments) == 0 {
		return nil
	}
	out := make([]segment, 0, len(segments))
	for _, seg := range segments {
		if seg.end <= seg.start {
			continue
		}
		if minSegment > 0 && seg.end-seg.start < minSegment {
			continue
		}
		out = append(out, seg)
	}
	return out
}

func splitTimingBySegments(t render.Timing, segments []segment) []render.Timing {
	if len(segments) == 0 {
		return nil
	}
	if len(segments) == 1 {
		return []render.Timing{{
			Verse: t.Verse,
			Start: segments[0].start,
			End:   segments[0].end,
		}}
	}
	arabicWords := strings.Fields(t.Verse.Text)
	transWords := strings.Fields(t.Verse.Translation)
	segCount := len(segments)
	if len(arabicWords) > 0 && len(arabicWords) < segCount {
		segCount = len(arabicWords)
	}
	if len(transWords) > 0 && len(transWords) < segCount {
		segCount = len(transWords)
	}
	if segCount <= 0 {
		segCount = 1
	}
	if segCount < len(segments) {
		segments = mergeSegments(segments, segCount)
	}
	durations := segmentDurations(segments)
	counts := allocateCounts(len(arabicWords), durations)
	segments, counts = mergeTinySegments(arabicWords, segments, counts)
	arabicParts := splitTextByCounts(arabicWords, counts)
	var transParts []string
	if len(transWords) > 0 {
		transCounts := allocateCounts(len(transWords), segmentDurations(segments))
		transParts = splitTextByCounts(transWords, transCounts)
	}
	out := make([]render.Timing, 0, len(segments))
	for i, seg := range segments {
		verse := t.Verse
		if i < len(arabicParts) && arabicParts[i] != "" {
			verse.Text = arabicParts[i]
		}
		if len(transParts) > 0 && i < len(transParts) {
			verse.Translation = transParts[i]
		}
		out = append(out, render.Timing{
			Verse: verse,
			Start: seg.start,
			End:   seg.end,
		})
	}
	return out
}

func splitTimingByWordTimings(t render.Timing, silences []audio.Silence, minSegment time.Duration) []render.Timing {
	words := strings.Fields(t.Verse.Text)
	if len(words) == 0 || len(t.WordTimings) == 0 {
		return nil
	}
	n := len(words)
	if len(t.WordTimings) < n {
		n = len(t.WordTimings)
	}
	words = words[:n]
	wordTimings := t.WordTimings[:n]
	boundaries := boundariesFromSilence(words, wordTimings, t.Start, t.End, silences)
	if len(boundaries) == 0 {
		return nil
	}
	counts := countsFromBoundaries(n, boundaries)
	segments := segmentsFromWordCounts(wordTimings, counts)
	segments, counts = mergeTinySegments(words, segments, counts)
	segments, counts = mergeShortSegments(segments, counts, minSegment)
	if len(segments) == 0 {
		return nil
	}
	arabicParts := splitTextByCounts(words, counts)
	transWords := strings.Fields(t.Verse.Translation)
	var transParts []string
	if len(transWords) > 0 {
		transCounts := allocateCounts(len(transWords), countsToDurations(counts))
		transParts = splitTextByCounts(transWords, transCounts)
	}
	out := make([]render.Timing, 0, len(segments))
	for i, seg := range segments {
		verse := t.Verse
		if i < len(arabicParts) && arabicParts[i] != "" {
			verse.Text = arabicParts[i]
		}
		if len(transParts) > 0 && i < len(transParts) {
			verse.Translation = transParts[i]
		}
		out = append(out, render.Timing{
			Verse: verse,
			Start: seg.start,
			End:   seg.end,
		})
	}
	return out
}

func boundariesFromSilence(words []string, wordTimings []render.WordTiming, start, end time.Duration, silences []audio.Silence) []int {
	if len(words) == 0 || len(wordTimings) == 0 {
		return nil
	}
	boundaries := make([]int, 0, 4)
	lastIdx := 0
	for _, s := range silences {
		if s.Start <= start || s.Start >= end {
			continue
		}
		idx := lastWordBefore(wordTimings, s.Start)
		if idx <= lastIdx || idx >= len(wordTimings) {
			continue
		}
		if isTinySegment(words[lastIdx:idx]) || isTinySegment(words[idx:]) {
			continue
		}
		boundaries = append(boundaries, idx)
		lastIdx = idx
	}
	return boundaries
}

func lastWordBefore(wordTimings []render.WordTiming, moment time.Duration) int {
	for i := len(wordTimings) - 1; i >= 0; i-- {
		if wordTimings[i].End <= moment {
			return i + 1
		}
	}
	return 0
}

func countsFromBoundaries(total int, boundaries []int) []int {
	if total <= 0 {
		return nil
	}
	counts := make([]int, 0, len(boundaries)+1)
	prev := 0
	for _, b := range boundaries {
		if b <= prev {
			continue
		}
		counts = append(counts, b-prev)
		prev = b
	}
	if prev < total {
		counts = append(counts, total-prev)
	}
	return counts
}

func segmentsFromWordCounts(wordTimings []render.WordTiming, counts []int) []segment {
	segments := make([]segment, 0, len(counts))
	cursor := 0
	for _, c := range counts {
		if c <= 0 {
			continue
		}
		if cursor+c > len(wordTimings) {
			c = len(wordTimings) - cursor
		}
		if c <= 0 {
			continue
		}
		start := wordTimings[cursor].Start
		end := wordTimings[cursor+c-1].End
		segments = append(segments, segment{start: start, end: end})
		cursor += c
	}
	return segments
}

func countsToDurations(counts []int) []time.Duration {
	durations := make([]time.Duration, 0, len(counts))
	for _, c := range counts {
		durations = append(durations, time.Duration(c))
	}
	return durations
}

func splitTextByDurations(words []string, durations []time.Duration) []string {
	if len(durations) == 0 {
		return nil
	}
	if len(words) == 0 {
		parts := make([]string, len(durations))
		return parts
	}
	counts := allocateCounts(len(words), durations)
	parts := make([]string, 0, len(counts))
	cursor := 0
	for _, c := range counts {
		if c <= 0 {
			parts = append(parts, "")
			continue
		}
		if cursor+c > len(words) {
			c = len(words) - cursor
		}
		if c <= 0 {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, strings.Join(words[cursor:cursor+c], " "))
		cursor += c
	}
	if cursor < len(words) && len(parts) > 0 {
		parts[len(parts)-1] = strings.TrimSpace(parts[len(parts)-1] + " " + strings.Join(words[cursor:], " "))
	}
	return parts
}

func splitTextByCounts(words []string, counts []int) []string {
	if len(counts) == 0 {
		return nil
	}
	if len(words) == 0 {
		return make([]string, len(counts))
	}
	parts := make([]string, 0, len(counts))
	cursor := 0
	for _, c := range counts {
		if c <= 0 {
			parts = append(parts, "")
			continue
		}
		if cursor+c > len(words) {
			c = len(words) - cursor
		}
		if c <= 0 {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, strings.Join(words[cursor:cursor+c], " "))
		cursor += c
	}
	if cursor < len(words) && len(parts) > 0 {
		parts[len(parts)-1] = strings.TrimSpace(parts[len(parts)-1] + " " + strings.Join(words[cursor:], " "))
	}
	return parts
}

func allocateCounts(totalWords int, durations []time.Duration) []int {
	if totalWords <= 0 || len(durations) == 0 {
		return nil
	}
	counts := make([]int, len(durations))
	remainingWords := totalWords
	remainingDur := time.Duration(0)
	for _, d := range durations {
		remainingDur += d
	}
	for i, d := range durations {
		segmentsLeft := len(durations) - i
		if segmentsLeft <= 1 {
			counts[i] = remainingWords
			break
		}
		if remainingDur <= 0 {
			counts[i] = max(1, remainingWords-(segmentsLeft-1))
		} else {
			ratio := float64(d) / float64(remainingDur)
			target := int(math.Round(float64(remainingWords) * ratio))
			if target < 1 {
				target = 1
			}
			maxAllowed := remainingWords - (segmentsLeft - 1)
			if target > maxAllowed {
				target = maxAllowed
			}
			counts[i] = target
		}
		remainingWords -= counts[i]
		remainingDur -= d
	}
	return counts
}

func mergeTinySegments(words []string, segments []segment, counts []int) ([]segment, []int) {
	if len(segments) == 0 || len(counts) == 0 || len(segments) != len(counts) {
		return segments, counts
	}
	for {
		idx := tinySegmentIndex(words, counts)
		if idx == -1 || len(counts) <= 1 {
			break
		}
		if idx < len(counts)-1 {
			counts[idx+1] += counts[idx]
			segments[idx+1].start = segments[idx].start
		} else {
			counts[idx-1] += counts[idx]
			segments[idx-1].end = segments[idx].end
		}
		counts = append(counts[:idx], counts[idx+1:]...)
		segments = append(segments[:idx], segments[idx+1:]...)
	}
	return segments, counts
}

func tinySegmentIndex(words []string, counts []int) int {
	cursor := 0
	for i, c := range counts {
		if c <= 0 {
			continue
		}
		if cursor+c > len(words) {
			c = len(words) - cursor
		}
		if c <= 0 {
			continue
		}
		segmentWords := words[cursor : cursor+c]
		if isTinySegment(segmentWords) {
			return i
		}
		cursor += c
	}
	return -1
}

func isTinySegment(words []string) bool {
	if len(words) == 0 {
		return true
	}
	if len(words) > 1 {
		return false
	}
	return wordLen(words[0]) <= 1
}

func wordLen(word string) int {
	count := 0
	for _, r := range word {
		if unicode.Is(unicode.Mn, r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		count++
	}
	return count
}

func mergeShortSegments(segments []segment, counts []int, minSegment time.Duration) ([]segment, []int) {
	if minSegment <= 0 || len(segments) <= 1 {
		return segments, counts
	}
	for {
		idx := shortSegmentIndex(segments, minSegment)
		if idx == -1 || len(segments) <= 1 {
			break
		}
		if idx < len(segments)-1 {
			segments[idx+1].start = segments[idx].start
			counts[idx+1] += counts[idx]
		} else {
			segments[idx-1].end = segments[idx].end
			counts[idx-1] += counts[idx]
		}
		segments = append(segments[:idx], segments[idx+1:]...)
		counts = append(counts[:idx], counts[idx+1:]...)
	}
	return segments, counts
}

func shortSegmentIndex(segments []segment, minSegment time.Duration) int {
	for i, seg := range segments {
		if seg.end-seg.start < minSegment {
			return i
		}
	}
	return -1
}

func mergeSegments(segments []segment, target int) []segment {
	if target <= 0 || len(segments) <= target {
		return segments
	}
	out := append([]segment(nil), segments...)
	for len(out) > target {
		idx := smallestSegmentIndex(out)
		if idx <= 0 {
			out[1].start = out[0].start
			out = append(out[:0], out[1:]...)
			continue
		}
		out[idx-1].end = out[idx].end
		out = append(out[:idx], out[idx+1:]...)
	}
	return out
}

func smallestSegmentIndex(segments []segment) int {
	if len(segments) == 0 {
		return 0
	}
	idx := 0
	best := segments[0].end - segments[0].start
	for i := 1; i < len(segments); i++ {
		d := segments[i].end - segments[i].start
		if d < best {
			best = d
			idx = i
		}
	}
	return idx
}
