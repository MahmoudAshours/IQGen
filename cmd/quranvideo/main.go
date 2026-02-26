package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	case "live":
		liveCmd(os.Args[2:])
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
  quranvideo live --surah 1 --start 1 --end 7 --mode full
  quranvideo live --yt-url https://www.youtube.com/watch?v=XXXX --stream --stream-url udp://127.0.0.1:23000
  quranvideo live --yt-url https://www.youtube.com/watch?v=XXXX --mode full --duration 120
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
	mode := fs.String("mode", "sequential", "Display mode: sequential|lines|repeat|repeat-2x2|word-by-word")
	output := fs.String("output", "", "Output video path")
	configPath := fs.String("config", "", "Config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logs for this run")
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
	logLevel := cfg.Logging.Level
	if *verbose {
		logLevel = "debug"
	}
	logger := utils.NewLogger(logLevel)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(*configPath))
	}

	ctx := context.Background()
	var result recognize.Result
	var precomputedWords []align.WordTiming
	if *surah > 0 && *startAyah > 0 && *endAyah > 0 {
		result = recognize.Result{Surah: *surah, StartAyah: *startAyah, EndAyah: *endAyah}
		logger.Infof("Using provided recitation range: Surah %d, Ayahs %d-%d", result.Surah, result.StartAyah, result.EndAyah)
	} else {
		audioForIdentify := *audioPath
		if cfg.Audio.EchoReduction {
			cleanDir := filepath.Join(cfg.Output.TempDir, "whisper_clean")
			if err := utils.EnsureDir(cleanDir); err == nil {
				cleanPath := filepath.Join(cleanDir, "identify_input.wav")
				if err := audio.EnhanceForSpeechRecognition(ctx, *audioPath, cleanPath, cfg.Audio.EchoFilter); err == nil {
					audioForIdentify = cleanPath
					logger.Infof("Applied echo reduction for recitation identification")
				}
			}
		}
		matcher := recognize.Matcher{
			Corpus:        newRecognizeCorpus(cfg.QuranAPI),
			ExpectedSurah: *expectedSurah,
		}
		timeout := sttTimeout(cfg.Audio)
		logger.Infof("Identifying recitation: backend=%s cmd=%s timeout=%s", sttBackendLabel(cfg.Audio), sttCommandLabel(cfg.Audio), timeout)

		// Fast path: use one STT pass with word timestamps for both identify and later alignment.
		aligner := newIdentifyAligner(cfg.Audio)
		if aligner.Available() {
			identifyCtx, cancel := context.WithTimeout(ctx, timeout)
			started := time.Now()
			detected, transcript, words, err := identifyFromWordTranscription(identifyCtx, aligner, audioForIdentify, cfg.Audio.Language, &matcher)
			cancel()
			logger.Infof("Identify stage finished in %s", time.Since(started).Round(time.Millisecond))
			if err == nil {
				result = detected
				precomputedWords = words
				logger.Debugf("Identification transcript length=%d words=%d", len(transcript), len(words))
				logger.Infof("Detected recitation: Surah %d, Ayahs %d-%d", result.Surah, result.StartAyah, result.EndAyah)
			} else {
				logger.Warnf("Word-timestamp identification failed: %v; falling back", err)
			}
		}
		if result.Surah == 0 {
			recognizer := newRecognizer(cfg.Audio)
			if !recognizer.Available() {
				exitWithError(sttUnavailableError(cfg.Audio))
			}
			identifyCtx, cancel := context.WithTimeout(ctx, timeout)
			started := time.Now()
			detected, transcript, err := recognizer.Identify(identifyCtx, audioForIdentify, cfg.Audio.Language, &matcher)
			cancel()
			logger.Infof("Identify stage finished in %s", time.Since(started).Round(time.Millisecond))
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					exitWithError(fmt.Errorf("identify timed out after %s; try increasing audio.stt_timeout_sec and/or using a faster audio.go_whisper_identify_model", timeout))
				}
				logger.Warnf("Identify failed: %v", err)
				if transcript != "" {
					logger.Infof("Transcript: %s", transcript)
				}
				exitWithError(err)
			}
			result = detected
			logger.Infof("Detected recitation: Surah %d, Ayahs %d-%d", result.Surah, result.StartAyah, result.EndAyah)
		}
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
	if len(precomputedWords) > 0 && !cfg.Audio.TrimSilence && !cfg.Audio.EchoReduction {
		opts.PrecomputedWords = precomputedWords
	}
	if err := runGenerate(opts); err != nil {
		exitWithError(err)
	}
}

func liveCmd(args []string) {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	surah := fs.Int("surah", 0, "Surah number (1-114) for fixed-range live mode")
	startAyah := fs.Int("start", 0, "Start ayah in surah for fixed-range live mode")
	endAyah := fs.Int("end", 0, "End ayah in surah for fixed-range live mode")
	expectedSurah := fs.Int("expected-surah", 0, "Optional expected surah (1-114) for stream ayah detection")
	mode := fs.String("mode", "full", "Live mode: full|2-word")
	durationSec := fs.Int("duration", 60, "Live capture duration in seconds")
	chunkSec := fs.Float64("chunk-sec", 2.0, "Whisper chunk size in seconds")
	stream := fs.Bool("stream", false, "Stream output mode (input stream -> output stream with live ayah text)")
	streamURL := fs.String("stream-url", "udp://127.0.0.1:23000?pkt_size=1316", "Output stream URL (udp:// or rtmp://)")
	streamFormat := fs.String("stream-format", "mpegts", "Output stream format: mpegts|flv|matroska")
	ytURL := fs.String("yt-url", "", "YouTube URL to use as live audio source (requires yt-dlp)")
	audioURL := fs.String("audio-url", "", "Direct live audio stream URL")
	ytDlpCmd := fs.String("yt-dlp-cmd", "yt-dlp", "yt-dlp command path")
	micDevice := fs.String("mic-device", "", "Microphone device input (platform specific)")
	backgroundPath := fs.String("background", "", "Custom background video path")
	noBackground := fs.Bool("no-background", false, "Disable background video (solid color)")
	output := fs.String("output", "", "Output video path")
	configPath := fs.String("config", "", "Config file path")
	_ = fs.Parse(args)

	if *durationSec < 0 {
		exitWithError(fmt.Errorf("duration must be >= 0"))
	}
	if *expectedSurah < 0 || *expectedSurah > 114 {
		exitWithError(fmt.Errorf("expected-surah must be between 1 and 114 when set"))
	}
	if *chunkSec < 0.7 {
		exitWithError(fmt.Errorf("chunk-sec must be >= 0.7"))
	}
	if strings.TrimSpace(*ytURL) != "" && strings.TrimSpace(*audioURL) != "" {
		exitWithError(fmt.Errorf("use only one of --yt-url or --audio-url"))
	}
	hasFixedRange := *surah > 0 || *startAyah > 0 || *endAyah > 0
	if hasFixedRange {
		if *surah < 1 || *surah > 114 {
			exitWithError(fmt.Errorf("surah must be between 1 and 114"))
		}
		if *startAyah <= 0 || *endAyah <= 0 || *endAyah < *startAyah {
			exitWithError(fmt.Errorf("invalid ayah range: start/end must be positive and end >= start"))
		}
	}
	if !hasFixedRange && strings.TrimSpace(*ytURL) == "" && strings.TrimSpace(*audioURL) == "" {
		exitWithError(fmt.Errorf("provide either a fixed ayah range (--surah/--start/--end) or a stream source (--yt-url/--audio-url)"))
	}
	if !*stream && *durationSec == 0 {
		exitWithError(fmt.Errorf("duration must be > 0 when --stream is false"))
	}
	if *stream && strings.TrimSpace(*streamURL) == "" {
		exitWithError(fmt.Errorf("stream-url is required in --stream mode"))
	}
	switch strings.ToLower(strings.TrimSpace(*streamFormat)) {
	case "mpegts", "flv", "matroska":
	default:
		exitWithError(fmt.Errorf("unsupported stream-format: %s", *streamFormat))
	}
	liveMode, err := parseLiveMode(*mode)
	if err != nil {
		exitWithError(err)
	}
	opts := liveOptions{
		HasFixedRange:  hasFixedRange,
		Surah:          *surah,
		StartAyah:      *startAyah,
		EndAyah:        *endAyah,
		ExpectedSurah:  *expectedSurah,
		Mode:           liveMode,
		Duration:       time.Duration(*durationSec) * time.Second,
		ChunkSec:       *chunkSec,
		Stream:         *stream,
		StreamURL:      strings.TrimSpace(*streamURL),
		StreamFormat:   strings.ToLower(strings.TrimSpace(*streamFormat)),
		YouTubeURL:     strings.TrimSpace(*ytURL),
		AudioURL:       strings.TrimSpace(*audioURL),
		YTDLPCmd:       strings.TrimSpace(*ytDlpCmd),
		MicDevice:      *micDevice,
		BackgroundPath: *backgroundPath,
		NoBackground:   *noBackground,
		Output:         *output,
		ConfigPath:     *configPath,
	}
	if err := runLive(opts); err != nil {
		exitWithError(err)
	}
}

func identifyCmd(args []string) {
	fs := flag.NewFlagSet("identify", flag.ExitOnError)
	audioPath := fs.String("audio", "", "Recitation audio file")
	expectedSurah := fs.Int("expected-surah", 0, "Optional expected surah number (1-114)")
	configPath := fs.String("config", "", "Config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logs for this run")
	_ = fs.Parse(args)
	if *audioPath == "" {
		exitWithError(fmt.Errorf("audio path is required"))
	}
	cfg, created, err := loadConfig(*configPath)
	if err != nil {
		exitWithError(err)
	}
	logLevel := cfg.Logging.Level
	if *verbose {
		logLevel = "debug"
	}
	logger := utils.NewLogger(logLevel)
	if created {
		logger.Infof("Created default config at %s", resolveConfigPath(*configPath))
	}
	recognizer := newRecognizer(cfg.Audio)
	if !recognizer.Available() {
		exitWithError(sttUnavailableError(cfg.Audio))
	}
	ctx := context.Background()
	matcher := recognize.Matcher{
		Corpus:        newRecognizeCorpus(cfg.QuranAPI),
		ExpectedSurah: *expectedSurah,
	}
	audioForIdentify := *audioPath
	if cfg.Audio.EchoReduction {
		cleanDir := filepath.Join(cfg.Output.TempDir, "whisper_clean")
		if err := utils.EnsureDir(cleanDir); err == nil {
			cleanPath := filepath.Join(cleanDir, "identify_single_input.wav")
			if err := audio.EnhanceForSpeechRecognition(ctx, *audioPath, cleanPath, cfg.Audio.EchoFilter); err == nil {
				audioForIdentify = cleanPath
				logger.Infof("Applied echo reduction for identification")
			}
		}
	}
	identifyCtx, cancel := context.WithTimeout(ctx, sttTimeout(cfg.Audio))
	defer cancel()
	logger.Infof("Identifying recitation: backend=%s cmd=%s timeout=%s", sttBackendLabel(cfg.Audio), sttCommandLabel(cfg.Audio), sttTimeout(cfg.Audio))
	started := time.Now()
	result, transcript, err := recognizer.Identify(identifyCtx, audioForIdentify, cfg.Audio.Language, &matcher)
	logger.Infof("Identify stage finished in %s", time.Since(started).Round(time.Millisecond))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			exitWithError(fmt.Errorf("identify timed out after %s; try increasing audio.stt_timeout_sec", sttTimeout(cfg.Audio)))
		}
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
	PrecomputedWords   []align.WordTiming
}

type liveOptions struct {
	HasFixedRange  bool
	Surah          int
	StartAyah      int
	EndAyah        int
	ExpectedSurah  int
	Mode           string
	Duration       time.Duration
	ChunkSec       float64
	Stream         bool
	StreamURL      string
	StreamFormat   string
	YouTubeURL     string
	AudioURL       string
	YTDLPCmd       string
	MicDevice      string
	BackgroundPath string
	NoBackground   bool
	Output         string
	ConfigPath     string
}

func generateCmd(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	opts := generateOptions{}
	fs.IntVar(&opts.Surah, "surah", 1, "Surah number (1-114)")
	fs.IntVar(&opts.StartAyah, "start", 1, "Start ayah in surah")
	fs.IntVar(&opts.EndAyah, "end", 1, "End ayah in surah")
	fs.StringVar(&opts.Mode, "mode", "sequential", "Display mode: sequential|lines|word-by-word")
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
	if cfg.QuranAPI.UseLocalDB {
		logger.Infof("Quran source: local DB (%s)", cfg.QuranAPI.LocalDBPath)
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
	client := newVerseSource(cfg.QuranAPI)
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
	whisperAudioPath, whisperSegments := prepareWhisperInputs(ctx, audioPath, segments, tempDir, cfg.Audio, logger)
	mode := strings.ToLower(opts.Mode)
	usedPrecomputedAlignment := false
	repeatPairs := isRepeatPairsMode(mode)
	if isRepeatMode(mode) && opts.AudioPath != "" {
		repeatTimings, err := buildRepeatTimings(ctx, verses, whisperAudioPath, audioDuration, cfg.Audio, logger, repeatPairs)
		if err != nil {
			logger.Warnf("Repeat mode failed: %v; falling back to sequential", err)
			mode = "sequential"
			opts.Mode = "sequential"
		} else {
			timings = repeatTimings
		}
	}
	if !isRepeatMode(mode) && opts.AudioPath != "" && len(opts.PrecomputedWords) > 0 {
		if applyPrecomputedWordAlignmentFullAudio(ctx, timings, opts.PrecomputedWords, whisperAudioPath, cfg.Audio, logger) {
			usedPrecomputedAlignment = true
			logger.Infof("Reused identify transcription for word alignment")
		}
	}
	if !repeatPairs && (isWordMode(mode) || isLinesMode(mode)) {
		if opts.AudioPath != "" {
			if !usedPrecomputedAlignment {
				_ = applyWordAlignmentFullAudio(ctx, timings, whisperAudioPath, cfg.Audio, logger)
			}
		} else {
			_ = applyWordAlignment(ctx, timings, whisperSegments, whisperAudioPath, cfg.Audio, logger)
		}
	}
	if isWordMode(mode) || isLinesMode(mode) || repeatPairs {
		normalizeWordTimings(timings)
	}
	if opts.AudioPath != "" && mode == "sequential" {
		aligned := usedPrecomputedAlignment
		if !aligned {
			aligned = applyWordAlignmentFullAudio(ctx, timings, whisperAudioPath, cfg.Audio, logger)
		}
		if aligned {
			if applyAyahBoundariesFromWordTimings(timings) {
				logger.Infof("Aligned ayah boundaries to recitation audio")
			}
		}
	}
	if mode == "sequential" && cfg.Audio.PauseSensitive {
		ensureWordTimings(ctx, opts.AudioPath != "", timings, whisperSegments, whisperAudioPath, cfg.Audio, logger)
		silences, err := audio.DetectSilences(ctx, audioPath, cfg.Audio.PauseDB, cfg.Audio.PauseSec)
		if err != nil {
			logger.Warnf("Pause-sensitive display failed: %v", err)
		} else if len(silences) > 0 {
			timings = splitTimingsOnSilence(timings, silences, 120*time.Millisecond)
		}
	} else if mode == "sequential" {
		ensureContinuousTimings(timings, audioDuration)
	} else if isLinesMode(mode) {
		ensureWordTimings(ctx, opts.AudioPath != "", timings, whisperSegments, whisperAudioPath, cfg.Audio, logger)
		lineTimings, err := buildLineModeTimings(opts.Surah, opts.StartAyah, opts.EndAyah, timings)
		if err != nil {
			logger.Warnf("Lines mode failed: %v; falling back to sequential", err)
			mode = "sequential"
			opts.Mode = "sequential"
			ensureContinuousTimings(timings, audioDuration)
		} else {
			timings = lineTimings
		}
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

	cmd := exec.CommandContext(context.Background(), "ffmpeg", args...)
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
	if err := cmd.Run(); err != nil {
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
		args, err := liveStreamInputArgs(opts.AudioURL)
		return args, "audio-url", err
	}
	if strings.TrimSpace(opts.YouTubeURL) != "" {
		streamURL, err := background.ResolveYouTubeAudioStreamURL(ctx, opts.YouTubeURL, opts.YTDLPCmd)
		if err != nil {
			return nil, "", err
		}
		args, err := liveStreamInputArgs(streamURL)
		return args, "youtube", err
	}
	args, err := liveMicInputArgs(opts.MicDevice)
	return args, "mic", err
}

func liveStreamInputArgs(streamURL string) ([]string, error) {
	u := strings.TrimSpace(streamURL)
	if u == "" {
		return nil, fmt.Errorf("empty stream URL")
	}
	return []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "2",
		"-i", u,
	}, nil
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
		files, _ := filepath.Glob(filepath.Join(chunksDir, "chunk_*.wav"))
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

		files, _ := filepath.Glob(filepath.Join(chunksDir, "chunk_*.wav"))
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

func newAligner(cfg config.AudioConfig) *align.WhisperAligner {
	return align.NewWhisperAlignerWithOptions(align.Options{
		Command:         cfg.WhisperCmd,
		Backend:         cfg.STTBackend,
		GoWhisperModel:  cfg.GoWhisperModel,
		GoWhisperRemote: cfg.GoWhisperRemote,
		GoWhisperAddr:   cfg.GoWhisperAddr,
	})
}

func newIdentifyAligner(cfg config.AudioConfig) *align.WhisperAligner {
	model := strings.TrimSpace(cfg.GoWhisperIdentifyModel)
	if model == "" {
		model = cfg.GoWhisperModel
	}
	return align.NewWhisperAlignerWithOptions(align.Options{
		Command:         cfg.WhisperCmd,
		Backend:         cfg.STTBackend,
		GoWhisperModel:  model,
		GoWhisperRemote: cfg.GoWhisperRemote,
		GoWhisperAddr:   cfg.GoWhisperAddr,
	})
}

func newRecognizer(cfg config.AudioConfig) *recognize.WhisperRecognizer {
	return recognize.NewWhisperRecognizerWithOptions(recognize.WhisperOptions{
		Command:                cfg.WhisperCmd,
		Backend:                cfg.STTBackend,
		GoWhisperModel:         cfg.GoWhisperModel,
		GoWhisperIdentifyModel: cfg.GoWhisperIdentifyModel,
		GoWhisperRemote:        cfg.GoWhisperRemote,
		GoWhisperAddr:          cfg.GoWhisperAddr,
	})
}

func sttBackendLabel(cfg config.AudioConfig) string {
	backend := strings.TrimSpace(strings.ToLower(cfg.STTBackend))
	if backend == "" {
		backend = "auto"
	}
	return backend
}

func sttCommandLabel(cfg config.AudioConfig) string {
	cmd := strings.TrimSpace(cfg.WhisperCmd)
	if cmd == "" {
		cmd = "whisper"
	}
	return cmd
}

func sttTimeout(cfg config.AudioConfig) time.Duration {
	sec := cfg.STTTimeoutSec
	if sec <= 0 {
		sec = 90
	}
	return time.Duration(sec) * time.Second
}

func sttUnavailableError(cfg config.AudioConfig) error {
	return fmt.Errorf("stt backend %q command not found in PATH: %s", sttBackendLabel(cfg), sttCommandLabel(cfg))
}

func newVerseSource(cfg config.QuranAPIConfig) quran.VerseSource {
	if cfg.UseLocalDB {
		return quran.NewDBClient(cfg.LocalDBPath)
	}
	return quran.NewClient(cfg.BaseURL, time.Duration(cfg.TimeoutSec)*time.Second)
}

func newRecognizeCorpus(cfg config.QuranAPIConfig) recognize.QuranCorpus {
	return recognize.NewCorpusFromConfig(
		cfg.BaseURL,
		cfg.Edition,
		time.Duration(cfg.TimeoutSec)*time.Second,
		cfg.UseLocalDB,
		cfg.LocalDBPath,
	)
}

func identifyFromWordTranscription(ctx context.Context, aligner *align.WhisperAligner, audioPath, language string, matcher *recognize.Matcher) (recognize.Result, string, []align.WordTiming, error) {
	if aligner == nil {
		return recognize.Result{}, "", nil, fmt.Errorf("aligner is nil")
	}
	if matcher == nil {
		return recognize.Result{}, "", nil, fmt.Errorf("matcher is nil")
	}
	words, err := aligner.TranscribeWordsContext(ctx, audioPath, language)
	if err != nil {
		return recognize.Result{}, "", nil, err
	}
	transcript := transcriptFromWordTimings(words)
	if strings.TrimSpace(transcript) == "" {
		return recognize.Result{}, "", words, fmt.Errorf("empty transcription")
	}
	result, err := matcher.Identify(ctx, transcript)
	if err != nil {
		return recognize.Result{}, transcript, words, err
	}
	return result, transcript, words, nil
}

func transcriptFromWordTimings(words []align.WordTiming) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		text := strings.TrimSpace(w.Word)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

func prepareWhisperInputs(ctx context.Context, fullAudioPath string, segments []audio.Segment, tempDir string, cfg config.AudioConfig, logger *utils.Logger) (string, []audio.Segment) {
	if !cfg.EchoReduction {
		return fullAudioPath, segments
	}
	if err := utils.EnsureDir(tempDir); err != nil {
		logger.Warnf("Echo reduction setup failed: %v", err)
		return fullAudioPath, segments
	}
	cleanDir := filepath.Join(tempDir, "whisper_clean")
	if err := utils.EnsureDir(cleanDir); err != nil {
		logger.Warnf("Echo reduction setup failed: %v", err)
		return fullAudioPath, segments
	}
	cleanAudioPath := filepath.Join(cleanDir, "full.wav")
	if err := audio.EnhanceForSpeechRecognition(ctx, fullAudioPath, cleanAudioPath, cfg.EchoFilter); err != nil {
		logger.Warnf("Echo reduction failed for full audio: %v", err)
		return fullAudioPath, segments
	}
	cleanSegments := make([]audio.Segment, len(segments))
	copy(cleanSegments, segments)
	jobs := make(chan int, len(cleanSegments))
	workers := whisperWorkerCount(cfg, len(cleanSegments))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				seg := cleanSegments[idx]
				if strings.TrimSpace(seg.Path) == "" {
					continue
				}
				dest := filepath.Join(cleanDir, fmt.Sprintf("seg_%d.wav", seg.AyahNumber))
				if err := audio.EnhanceForSpeechRecognition(ctx, seg.Path, dest, cfg.EchoFilter); err == nil {
					cleanSegments[idx].Path = dest
				}
			}
		}()
	}
	for idx := range cleanSegments {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	logger.Infof("Echo reduction prepared for whisper alignment")
	return cleanAudioPath, cleanSegments
}

func whisperWorkerCount(cfg config.AudioConfig, jobs int) int {
	if jobs <= 0 {
		return 1
	}
	workers := cfg.WhisperWorkers
	if workers <= 0 {
		workers = runtime.NumCPU() / 2
		if workers < 1 {
			workers = 1
		}
	}
	if workers > jobs {
		return jobs
	}
	return workers
}

func applyWordAlignment(ctx context.Context, timings []render.Timing, segments []audio.Segment, audioPath string, cfg config.AudioConfig, logger *utils.Logger) bool {
	mode := strings.ToLower(cfg.WordTiming)
	if mode == "" {
		mode = "auto"
	}
	if mode == "even" {
		return false
	}
	aligner := newAligner(cfg)
	if !aligner.Available() {
		if mode == "whisper" {
			logger.Warnf("STT backend unavailable; falling back to even word timing (%v)", sttUnavailableError(cfg))
		}
		return false
	}
	type job struct {
		idx   int
		path  string
		words []string
		base  time.Duration
		ayah  int
	}
	type result struct {
		idx   int
		words []render.WordTiming
		err   error
		ayah  int
	}
	jobs := make([]job, 0, len(timings))
	for i := range timings {
		if i >= len(segments) {
			break
		}
		words := strings.Fields(timings[i].Verse.Text)
		if len(words) == 0 || strings.TrimSpace(segments[i].Path) == "" {
			continue
		}
		jobs = append(jobs, job{
			idx:   i,
			path:  segments[i].Path,
			words: words,
			base:  timings[i].Start,
			ayah:  timings[i].Verse.NumberInSurah,
		})
	}
	if len(jobs) == 0 {
		return false
	}
	workers := whisperWorkerCount(cfg, len(jobs))
	jobCh := make(chan job, len(jobs))
	resCh := make(chan result, len(jobs))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				wordTimings, err := aligner.AlignContext(ctx, j.path, j.words, cfg.Language)
				if err != nil {
					resCh <- result{idx: j.idx, err: err, ayah: j.ayah}
					continue
				}
				mapped := make([]render.WordTiming, 0, len(wordTimings))
				for _, wt := range wordTimings {
					mapped = append(mapped, render.WordTiming{
						Word:  wt.Word,
						Start: j.base + wt.Start,
						End:   j.base + wt.End,
					})
				}
				resCh <- result{idx: j.idx, words: mapped, ayah: j.ayah}
			}
		}()
	}
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	wg.Wait()
	close(resCh)
	alignedAny := false
	for r := range resCh {
		if r.err != nil {
			logger.Warnf("Word alignment failed for ayah %d: %v; using even split", r.ayah, r.err)
			continue
		}
		timings[r.idx].WordTimings = r.words
		if len(r.words) > 0 {
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
	aligner := newAligner(cfg)
	if !aligner.Available() {
		if mode == "whisper" {
			logger.Warnf("STT backend unavailable; falling back to even word timing (%v)", sttUnavailableError(cfg))
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
	wordTimings, err := aligner.AlignContext(ctx, audioPath, words, cfg.Language)
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

func applyPrecomputedWordAlignmentFullAudio(ctx context.Context, timings []render.Timing, whisperWords []align.WordTiming, audioPath string, cfg config.AudioConfig, logger *utils.Logger) bool {
	if len(whisperWords) == 0 {
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
	wordTimings, err := align.AlignWordsToTranscript(words, whisperWords)
	if err != nil {
		logger.Warnf("Precomputed word alignment failed: %v", err)
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

func isRepeatMode(mode string) bool {
	switch strings.ToLower(mode) {
	case "repeat", "sequential-repeat", "repeat-2x2", "repeat-two-by-two", "repeat-pair":
		return true
	default:
		return false
	}
}

func isLinesMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "line", "lines", "line-by-line":
		return true
	default:
		return false
	}
}

func isWordMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "word-by-word", "word", "two-by-two", "two", "pair", "2x2":
		return true
	default:
		return false
	}
}

func isRepeatPairsMode(mode string) bool {
	switch strings.ToLower(mode) {
	case "repeat-2x2", "repeat-two-by-two", "repeat-pair":
		return true
	default:
		return false
	}
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

func normalizeWordTimings(timings []render.Timing) {
	minDur := 100 * time.Millisecond
	for i := range timings {
		if len(timings[i].WordTimings) == 0 {
			continue
		}
		timings[i].WordTimings = collapseStandaloneMarks(timings[i].WordTimings)
		if len(timings[i].WordTimings) == 0 {
			continue
		}
		verseStart := timings[i].Start
		verseEnd := timings[i].End
		prevEnd := verseStart
		for j := range timings[i].WordTimings {
			start := timings[i].WordTimings[j].Start
			end := timings[i].WordTimings[j].End
			if start < verseStart {
				start = verseStart
			}
			if start < prevEnd {
				start = prevEnd
			}
			if end < start {
				end = start
			}
			if end-start < minDur {
				end = start + minDur
			}
			if end > verseEnd {
				verseEnd = end
			}
			timings[i].WordTimings[j].Start = start
			timings[i].WordTimings[j].End = end
			prevEnd = end
		}
		if verseEnd > timings[i].End {
			timings[i].End = verseEnd
		}
	}
}

func collapseStandaloneMarks(words []render.WordTiming) []render.WordTiming {
	if len(words) == 0 {
		return words
	}
	out := make([]render.WordTiming, 0, len(words))
	var pending string
	var pendingStart time.Duration
	var pendingEnd time.Duration
	hasPending := false
	for _, wt := range words {
		text := strings.TrimSpace(wt.Word)
		if text == "" || isStandaloneMark(text) {
			if len(out) > 0 {
				last := len(out) - 1
				out[last].Word += text
				if wt.End > out[last].End {
					out[last].End = wt.End
				}
				continue
			}
			if text == "" {
				continue
			}
			if !hasPending {
				pendingStart = wt.Start
				pendingEnd = wt.End
				hasPending = true
			} else if wt.End > pendingEnd {
				pendingEnd = wt.End
			}
			pending += text
			continue
		}
		next := wt
		if hasPending {
			next.Word = pending + next.Word
			if pendingStart < next.Start {
				next.Start = pendingStart
			}
			if pendingEnd > next.End {
				next.End = pendingEnd
			}
			hasPending = false
			pending = ""
		}
		out = append(out, next)
	}
	if hasPending {
		if len(out) > 0 {
			last := len(out) - 1
			out[last].Word += pending
			if pendingEnd > out[last].End {
				out[last].End = pendingEnd
			}
		} else {
			out = append(out, render.WordTiming{
				Word:  pending,
				Start: pendingStart,
				End:   pendingEnd,
			})
		}
	}
	return out
}

func isStandaloneMark(word string) bool {
	for _, r := range word {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return false
		}
	}
	return true
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

type ayahTokens struct {
	verse quran.Verse
	words []string
	norm  []string
}

type ayahMatch struct {
	start    int
	end      int
	matches  int
	length   int
	score    int
	coverage float64
}

func buildRepeatTimings(ctx context.Context, verses []quran.Verse, audioPath string, audioDuration time.Duration, cfg config.AudioConfig, logger *utils.Logger, withWordTimings bool) ([]render.Timing, error) {
	aligner := newAligner(cfg)
	if !aligner.Available() {
		return nil, sttUnavailableError(cfg)
	}
	whisperWords, err := aligner.TranscribeWordsContext(ctx, audioPath, cfg.Language)
	if err != nil {
		return nil, err
	}
	if len(whisperWords) == 0 {
		return nil, fmt.Errorf("no whisper words")
	}
	if audioDuration <= 0 {
		audioDuration = whisperWords[len(whisperWords)-1].End
	}
	silences, err := audio.DetectSilences(ctx, audioPath, cfg.PauseDB, cfg.PauseSec)
	if err != nil {
		logger.Warnf("Repeat mode silence detection failed: %v", err)
	}
	segments := []segment{{start: 0, end: audioDuration}}
	if len(silences) > 0 {
		segments = segmentsFromSilence(render.Timing{Start: 0, End: audioDuration}, silences)
		segments = filterSegments(segments, 120*time.Millisecond)
		if len(segments) == 0 {
			segments = []segment{{start: 0, end: audioDuration}}
		}
	}

	ayahs := make([]ayahTokens, 0, len(verses))
	for _, v := range verses {
		words := strings.Fields(v.Text)
		norm := make([]string, 0, len(words))
		for _, w := range words {
			n := align.NormalizeWord(w)
			if n == "" {
				norm = append(norm, "")
				continue
			}
			norm = append(norm, n)
		}
		ayahs = append(ayahs, ayahTokens{verse: v, words: words, norm: norm})
	}

	var timings []render.Timing
	current := 0
	for _, seg := range segments {
		segWords := wordsInSegment(whisperWords, seg)
		if len(segWords) == 0 {
			continue
		}
		segTokens := normalizeSegmentTokens(segWords)
		if len(segTokens) == 0 {
			continue
		}
		bestIdx, bestMatch := selectBestAyahMatch(segTokens, ayahs, current)
		if bestIdx < 0 {
			continue
		}
		if bestIdx > current {
			current = bestIdx
		}
		text, full := renderAyahMatchText(ayahs[bestIdx], bestMatch)
		verse := ayahs[bestIdx].verse
		if text != "" {
			verse.Text = text
		}
		if !full {
			verse.Translation = ""
		}
		start := segWords[0].Start
		end := segWords[len(segWords)-1].End
		if end < start {
			end = start
		}
		var wordTimings []render.WordTiming
		if withWordTimings {
			words := ayahs[bestIdx].words
			ws := sliceAyahWords(words, bestMatch.start, bestMatch.end)
			if len(ws) == 0 {
				ws = strings.Fields(verse.Text)
			}
			wordTimings = mapSegmentWordTimings(ws, segWords)
		}
		timings = append(timings, render.Timing{
			Verse:       verse,
			Start:       start,
			End:         end,
			WordTimings: wordTimings,
		})
	}
	if len(timings) == 0 {
		return nil, fmt.Errorf("no repeat timings produced")
	}
	return timings, nil
}

func wordsInSegment(words []align.WordTiming, seg segment) []align.WordTiming {
	out := make([]align.WordTiming, 0, len(words))
	for _, w := range words {
		mid := w.Start + (w.End-w.Start)/2
		if mid >= seg.start && mid <= seg.end {
			out = append(out, w)
		}
	}
	return out
}

func normalizeSegmentTokens(words []align.WordTiming) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		n := align.NormalizeWord(w.Word)
		if n == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

func selectBestAyahMatch(tokens []string, ayahs []ayahTokens, current int) (int, ayahMatch) {
	bestIdx := -1
	var best ayahMatch
	if current < 0 {
		current = 0
	}
	candIdx := current
	if candIdx < len(ayahs) {
		if m, ok := matchSegmentToAyah(tokens, ayahs[candIdx]); ok {
			bestIdx = candIdx
			best = m
		}
	}
	nextIdx := current + 1
	if nextIdx < len(ayahs) {
		if m, ok := matchSegmentToAyah(tokens, ayahs[nextIdx]); ok {
			if bestIdx == -1 || isBetterMatch(m, best) {
				bestIdx = nextIdx
				best = m
			}
		}
	}
	return bestIdx, best
}

func matchSegmentToAyah(tokens []string, ayah ayahTokens) (ayahMatch, bool) {
	if len(tokens) == 0 || len(ayah.norm) == 0 {
		return ayahMatch{}, false
	}
	start, end, matches, length, score := localAlignTokens(tokens, ayah.norm)
	if matches == 0 || start < 0 || end < 0 {
		return ayahMatch{}, false
	}
	coverage := float64(matches) / float64(len(tokens))
	minMatches := 1
	if len(tokens) >= 4 {
		minMatches = 2
	}
	if matches < minMatches && coverage < 0.2 {
		return ayahMatch{}, false
	}
	return ayahMatch{start: start, end: end, matches: matches, length: length, score: score, coverage: coverage}, true
}

func isBetterMatch(a, b ayahMatch) bool {
	if a.matches != b.matches {
		return a.matches > b.matches
	}
	if a.coverage != b.coverage {
		return a.coverage > b.coverage
	}
	if a.score != b.score {
		return a.score > b.score
	}
	return a.length > b.length
}

func renderAyahMatchText(ayah ayahTokens, match ayahMatch) (string, bool) {
	if len(ayah.words) == 0 {
		return "", false
	}
	start := match.start
	end := match.end
	if start < 0 {
		start = 0
	}
	if end >= len(ayah.words) {
		end = len(ayah.words) - 1
	}
	if end < start {
		return "", false
	}
	full := start == 0 && end == len(ayah.words)-1
	return strings.Join(ayah.words[start:end+1], " "), full
}

func sliceAyahWords(words []string, start, end int) []string {
	if len(words) == 0 {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if end >= len(words) {
		end = len(words) - 1
	}
	if end < start {
		return nil
	}
	return words[start : end+1]
}

func mapSegmentWordTimings(words []string, segWords []align.WordTiming) []render.WordTiming {
	if len(words) == 0 || len(segWords) == 0 {
		return nil
	}
	start := segWords[0].Start
	end := segWords[len(segWords)-1].End
	if end < start {
		end = start
	}
	if len(words) == len(segWords) {
		out := make([]render.WordTiming, len(words))
		for i := range words {
			out[i] = render.WordTiming{Word: words[i], Start: segWords[i].Start, End: segWords[i].End}
		}
		return out
	}
	ayahNorm := make([]string, 0, len(words))
	for _, w := range words {
		ayahNorm = append(ayahNorm, align.NormalizeWord(w))
	}
	segNorm := make([]string, 0, len(segWords))
	for _, w := range segWords {
		segNorm = append(segNorm, align.NormalizeWord(w.Word))
	}
	pairs := lcsIndexPairs(ayahNorm, segNorm)
	if len(pairs) == 0 {
		return evenSplitWordTimings(words, start, end)
	}
	out := make([]render.WordTiming, len(words))
	matched := make([]bool, len(words))
	for _, p := range pairs {
		if p.i < 0 || p.i >= len(words) || p.j < 0 || p.j >= len(segWords) {
			continue
		}
		out[p.i] = render.WordTiming{Word: words[p.i], Start: segWords[p.j].Start, End: segWords[p.j].End}
		matched[p.i] = true
	}
	fillMissingWordTimings(out, matched, start, end)
	return out
}

func evenSplitWordTimings(words []string, start, end time.Duration) []render.WordTiming {
	if len(words) == 0 || end <= start {
		return nil
	}
	per := (end - start) / time.Duration(len(words))
	if per <= 0 {
		return nil
	}
	out := make([]render.WordTiming, len(words))
	cursor := start
	for i := range words {
		s := cursor
		e := s + per
		if i == len(words)-1 {
			e = end
		}
		out[i] = render.WordTiming{Word: words[i], Start: s, End: e}
		cursor = e
	}
	return out
}

type lcsPair struct{ i, j int }

func lcsIndexPairs(a, b []string) []lcsPair {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] != "" && a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var pairs []lcsPair
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] != "" && a[i-1] == b[j-1] {
			pairs = append(pairs, lcsPair{i - 1, j - 1})
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
		pairs[i], pairs[j] = pairs[j], pairs[i]
	}
	return pairs
}

func fillMissingWordTimings(timings []render.WordTiming, matched []bool, start, end time.Duration) {
	if len(timings) == 0 {
		return
	}
	lastMatched := -1
	for i := 0; i < len(timings); i++ {
		if matched[i] {
			if i > lastMatched+1 {
				fillWordGap(timings, matched, lastMatched, i, start, end)
			}
			lastMatched = i
		}
	}
	if lastMatched < len(timings)-1 {
		fillWordGap(timings, matched, lastMatched, len(timings), start, end)
	}
	for i := range timings {
		if timings[i].End < timings[i].Start {
			timings[i].End = timings[i].Start
		}
	}
}

func fillWordGap(timings []render.WordTiming, matched []bool, prevIdx, nextIdx int, start, end time.Duration) {
	gapStart := start
	gapEnd := end
	if prevIdx >= 0 {
		gapStart = timings[prevIdx].End
	}
	if nextIdx < len(timings) && matched[nextIdx] {
		gapEnd = timings[nextIdx].Start
	}
	if gapEnd < gapStart {
		gapEnd = gapStart
	}
	count := nextIdx - prevIdx - 1
	if count <= 0 {
		return
	}
	step := time.Duration(0)
	if gapEnd > gapStart {
		step = (gapEnd - gapStart) / time.Duration(count+1)
	}
	cursor := gapStart
	for i := prevIdx + 1; i < nextIdx; i++ {
		s := cursor + step
		e := s + step
		if step == 0 {
			e = s
		}
		timings[i].Start = s
		timings[i].End = e
		matched[i] = true
		cursor = e
	}
}

type alignCell struct {
	score   int
	start   int
	matches int
	length  int
}

var ayahMarkerPattern = regexp.MustCompile(`\(\s*([0-9]+)\s*\)`)

func localAlignTokens(needles []string, haystack []string) (startIdx, endIdx, matches, length, score int) {
	n := len(needles)
	m := len(haystack)
	if n == 0 || m == 0 {
		return -1, -1, 0, 0, 0
	}
	prev := make([]alignCell, m+1)
	curr := make([]alignCell, m+1)
	best := alignCell{}
	bestEnd := -1
	for i := 1; i <= n; i++ {
		curr[0] = alignCell{}
		for j := 1; j <= m; j++ {
			matchScore := -1
			if needles[i-1] != "" && needles[i-1] == haystack[j-1] {
				matchScore = 2
			}

			var bestCell alignCell
			bestCell.start = j - 1

			diag := prev[j-1]
			scoreDiag := diag.score + matchScore
			if scoreDiag > 0 {
				start := diag.start
				if diag.score == 0 {
					start = j - 1
				}
				cand := alignCell{
					score:   scoreDiag,
					start:   start,
					matches: diag.matches + boolToInt(matchScore > 0),
					length:  diag.length + 1,
				}
				bestCell = pickBestCell(bestCell, cand)
			}

			up := prev[j]
			scoreUp := up.score - 1
			if scoreUp > 0 {
				start := up.start
				if up.score == 0 {
					start = j - 1
				}
				cand := alignCell{
					score:   scoreUp,
					start:   start,
					matches: up.matches,
					length:  up.length + 1,
				}
				bestCell = pickBestCell(bestCell, cand)
			}

			left := curr[j-1]
			scoreLeft := left.score - 1
			if scoreLeft > 0 {
				start := left.start
				if left.score == 0 {
					start = j - 1
				}
				cand := alignCell{
					score:   scoreLeft,
					start:   start,
					matches: left.matches,
					length:  left.length + 1,
				}
				bestCell = pickBestCell(bestCell, cand)
			}

			if bestCell.score < 0 {
				bestCell = alignCell{}
				bestCell.start = j - 1
			}
			curr[j] = bestCell
			if bestCell.score > best.score ||
				(bestCell.score == best.score && bestCell.matches > best.matches) ||
				(bestCell.score == best.score && bestCell.matches == best.matches && bestCell.length > best.length) {
				best = bestCell
				bestEnd = j - 1
			}
		}
		prev, curr = curr, prev
	}
	if best.score == 0 || bestEnd < 0 {
		return -1, -1, 0, 0, 0
	}
	return best.start, bestEnd, best.matches, best.length, best.score
}

func pickBestCell(current, cand alignCell) alignCell {
	if cand.score > current.score ||
		(cand.score == current.score && cand.matches > current.matches) ||
		(cand.score == current.score && cand.matches == current.matches && cand.length > current.length) {
		return cand
	}
	return current
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
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

type quranLinePart struct {
	Ayah int
	Text string
}

type quranLine struct {
	Text      string
	AyahStart int
	AyahEnd   int
}

type timedWord struct {
	Start time.Duration
	End   time.Duration
}

func buildLineModeTimings(surah, startAyah, endAyah int, timings []render.Timing) ([]render.Timing, error) {
	lines, err := loadQuranLinesForRange(surah, startAyah, endAyah)
	if err != nil {
		return nil, err
	}
	words := collectTimedArabicWords(timings)
	if len(words) == 0 {
		return nil, fmt.Errorf("lines mode requires word timings")
	}
	weights := make([]time.Duration, len(lines))
	for i, line := range lines {
		count := len(strings.Fields(line.Text))
		if count <= 0 {
			count = 1
		}
		weights[i] = time.Duration(count)
	}
	counts := allocateCounts(len(words), weights)
	if len(counts) == 0 {
		return nil, fmt.Errorf("failed to allocate line timings")
	}
	activeLines := make([]quranLine, 0, len(lines))
	activeCounts := make([]int, 0, len(counts))
	for i, c := range counts {
		if c <= 0 || i >= len(lines) {
			continue
		}
		activeLines = append(activeLines, lines[i])
		activeCounts = append(activeCounts, c)
	}
	if len(activeLines) == 0 {
		return nil, fmt.Errorf("no non-empty lines after timing allocation")
	}
	transWords := collectTranslationWords(timings)
	transCounts := allocateCounts(len(transWords), countsToDurations(activeCounts))
	baseVerse := timings[0].Verse
	verseByAyah := make(map[int]quran.Verse, len(timings))
	for _, t := range timings {
		verseByAyah[t.Verse.NumberInSurah] = t.Verse
	}
	out := make([]render.Timing, 0, len(activeLines))
	wordCursor := 0
	transCursor := 0
	for i, line := range activeLines {
		count := activeCounts[i]
		if wordCursor+count > len(words) {
			count = len(words) - wordCursor
		}
		if count <= 0 {
			continue
		}
		start := words[wordCursor].Start
		end := words[wordCursor+count-1].End
		if end < start {
			end = start
		}
		wordCursor += count

		verse := baseVerse
		if v, ok := verseByAyah[line.AyahStart]; ok {
			verse = v
		}
		verse.NumberInSurah = line.AyahStart
		verse.Text = strings.TrimSpace(line.Text)
		verse.Translation = ""
		if len(transWords) > 0 && i < len(transCounts) {
			take := transCounts[i]
			if transCursor+take > len(transWords) {
				take = len(transWords) - transCursor
			}
			if take > 0 {
				verse.Translation = strings.Join(transWords[transCursor:transCursor+take], " ")
				transCursor += take
			}
		}
		out = append(out, render.Timing{
			Verse: verse,
			Start: start,
			End:   end,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no line timings were produced")
	}
	return out, nil
}

func loadQuranLinesForRange(surah, startAyah, endAyah int) ([]quranLine, error) {
	path, err := resolveLinesFilePath(surah)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read line source %s: %w", path, err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	rawLines := strings.Split(content, "\n")
	currentAyah := 1
	lines := make([]quranLine, 0, len(rawLines))
	for _, raw := range rawLines {
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if line == "" {
			continue
		}
		parts, nextAyah := splitLineIntoAyahParts(line, currentAyah)
		currentAyah = nextAyah
		textParts := make([]string, 0, len(parts))
		ayahStart := 0
		ayahEnd := 0
		for _, part := range parts {
			if part.Ayah < startAyah || part.Ayah > endAyah {
				continue
			}
			p := strings.TrimSpace(part.Text)
			if p == "" {
				continue
			}
			textParts = append(textParts, p)
			if ayahStart == 0 || part.Ayah < ayahStart {
				ayahStart = part.Ayah
			}
			if part.Ayah > ayahEnd {
				ayahEnd = part.Ayah
			}
		}
		if len(textParts) == 0 || ayahStart == 0 {
			continue
		}
		lines = append(lines, quranLine{
			Text:      strings.Join(textParts, " "),
			AyahStart: ayahStart,
			AyahEnd:   ayahEnd,
		})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no lines found in %s for ayahs %d-%d", path, startAyah, endAyah)
	}
	return lines, nil
}

func resolveLinesFilePath(surah int) (string, error) {
	filename := fmt.Sprintf("%d.txt", surah)
	candidates := make([]string, 0, 4)
	if custom := strings.TrimSpace(os.Getenv("QURAN_LINES_DIR")); custom != "" {
		candidates = append(candidates, filepath.Join(custom, filename))
	}
	candidates = append(candidates,
		filepath.Join("lines", filename),
		filepath.Join("..", "lines", filename),
		filepath.Join("..", "..", "lines", filename),
	)
	for _, candidate := range candidates {
		if utils.FileExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("quran lines file not found for surah %d (tried %s)", surah, strings.Join(candidates, ", "))
}

func splitLineIntoAyahParts(line string, currentAyah int) ([]quranLinePart, int) {
	matches := ayahMarkerPattern.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return []quranLinePart{{Ayah: currentAyah, Text: strings.TrimSpace(line)}}, currentAyah
	}
	parts := make([]quranLinePart, 0, len(matches)+1)
	cursor := 0
	for _, m := range matches {
		if m[0] > cursor {
			text := strings.TrimSpace(line[cursor:m[0]])
			if text != "" {
				parts = append(parts, quranLinePart{Ayah: currentAyah, Text: text})
			}
		}
		nStr := strings.TrimSpace(line[m[2]:m[3]])
		if n, err := strconv.Atoi(nStr); err == nil && n > 0 {
			currentAyah = n + 1
		}
		cursor = m[1]
	}
	if cursor < len(line) {
		text := strings.TrimSpace(line[cursor:])
		if text != "" {
			parts = append(parts, quranLinePart{Ayah: currentAyah, Text: text})
		}
	}
	return parts, currentAyah
}

func collectTimedArabicWords(timings []render.Timing) []timedWord {
	out := make([]timedWord, 0, 512)
	for _, t := range timings {
		if len(t.WordTimings) > 0 {
			for _, wt := range t.WordTimings {
				text := strings.TrimSpace(wt.Word)
				if text == "" || isStandaloneMark(text) {
					continue
				}
				start := wt.Start
				end := wt.End
				if end < start {
					end = start
				}
				out = append(out, timedWord{Start: start, End: end})
			}
			continue
		}
		words := strings.Fields(t.Verse.Text)
		if len(words) == 0 {
			continue
		}
		total := t.End - t.Start
		if total < 0 {
			total = 0
		}
		per := time.Duration(0)
		if len(words) > 0 {
			per = total / time.Duration(len(words))
		}
		cursor := t.Start
		for i := range words {
			start := cursor
			end := start + per
			if i == len(words)-1 {
				end = t.End
			}
			if end < start {
				end = start
			}
			out = append(out, timedWord{Start: start, End: end})
			cursor = end
		}
	}
	return out
}

func collectTranslationWords(timings []render.Timing) []string {
	words := make([]string, 0, 512)
	for _, t := range timings {
		if strings.TrimSpace(t.Verse.Translation) == "" {
			continue
		}
		words = append(words, strings.Fields(t.Verse.Translation)...)
	}
	return words
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
