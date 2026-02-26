package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qgencodex/internal/ai"
	"qgencodex/internal/align"
	"qgencodex/internal/audio"
	"qgencodex/internal/background"
	"qgencodex/internal/batch"
	"qgencodex/internal/caption"
	"qgencodex/internal/config"
	"qgencodex/internal/ffmpeg"
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
	audioYTURL := fs.String("audio-yt-url", "", "YouTube URL to download recitation audio from")
	ytDlpCmd := fs.String("yt-dlp-cmd", "yt-dlp", "yt-dlp command path for YouTube audio download")
	removeFatiha := fs.Bool("remove-fatiha", false, "Remove leading Al-Fatihah if present before processing")
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

	if strings.TrimSpace(*audioPath) == "" && strings.TrimSpace(*audioYTURL) == "" {
		exitWithError(fmt.Errorf("either --audio or --audio-yt-url is required"))
	}
	if strings.TrimSpace(*audioPath) != "" && strings.TrimSpace(*audioYTURL) != "" {
		exitWithError(fmt.Errorf("use only one of --audio or --audio-yt-url"))
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
	inputAudioPath := strings.TrimSpace(*audioPath)
	if strings.TrimSpace(*audioYTURL) != "" {
		if err := utils.EnsureDir(cfg.Output.TempDir); err != nil {
			exitWithError(err)
		}
		downloadPath := filepath.Join(cfg.Output.TempDir, "input_from_youtube.mp3")
		logger.Infof("Downloading recitation audio from YouTube")
		downloaded, err := audio.DownloadYouTubeAudio(ctx, strings.TrimSpace(*audioYTURL), downloadPath, strings.TrimSpace(*ytDlpCmd))
		if err != nil {
			exitWithError(err)
		}
		inputAudioPath = downloaded
		logger.Infof("Using downloaded YouTube audio: %s", inputAudioPath)
	}
	if *removeFatiha {
		processedPath, removed, err := maybeRemoveLeadingFatiha(ctx, inputAudioPath, cfg, logger)
		if err != nil {
			logger.Warnf("Al-Fatihah removal failed: %v", err)
		} else if removed {
			inputAudioPath = processedPath
		}
	}
	var result recognize.Result
	var precomputedWords []align.WordTiming
	if *surah > 0 && *startAyah > 0 && *endAyah > 0 {
		result = recognize.Result{Surah: *surah, StartAyah: *startAyah, EndAyah: *endAyah}
		logger.Infof("Using provided recitation range: Surah %d, Ayahs %d-%d", result.Surah, result.StartAyah, result.EndAyah)
	} else {
		audioForIdentify := inputAudioPath
		if cfg.Audio.EchoReduction {
			cleanDir := filepath.Join(cfg.Output.TempDir, "whisper_clean")
			if err := utils.EnsureDir(cleanDir); err == nil {
				cleanPath := filepath.Join(cleanDir, "identify_input.wav")
				if err := audio.EnhanceForSpeechRecognition(ctx, inputAudioPath, cleanPath, cfg.Audio.EchoFilter); err == nil {
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
		AudioPath:          inputAudioPath,
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
	audioYTURL := fs.String("audio-yt-url", "", "YouTube URL to download recitation audio from")
	ytDlpCmd := fs.String("yt-dlp-cmd", "yt-dlp", "yt-dlp command path for YouTube audio download")
	removeFatiha := fs.Bool("remove-fatiha", false, "Remove leading Al-Fatihah if present before identification")
	expectedSurah := fs.Int("expected-surah", 0, "Optional expected surah number (1-114)")
	configPath := fs.String("config", "", "Config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logs for this run")
	_ = fs.Parse(args)
	if strings.TrimSpace(*audioPath) == "" && strings.TrimSpace(*audioYTURL) == "" {
		exitWithError(fmt.Errorf("either --audio or --audio-yt-url is required"))
	}
	if strings.TrimSpace(*audioPath) != "" && strings.TrimSpace(*audioYTURL) != "" {
		exitWithError(fmt.Errorf("use only one of --audio or --audio-yt-url"))
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
	inputAudioPath := strings.TrimSpace(*audioPath)
	if strings.TrimSpace(*audioYTURL) != "" {
		if err := utils.EnsureDir(cfg.Output.TempDir); err != nil {
			exitWithError(err)
		}
		downloadPath := filepath.Join(cfg.Output.TempDir, "identify_input_from_youtube.mp3")
		logger.Infof("Downloading recitation audio from YouTube")
		downloaded, err := audio.DownloadYouTubeAudio(ctx, strings.TrimSpace(*audioYTURL), downloadPath, strings.TrimSpace(*ytDlpCmd))
		if err != nil {
			exitWithError(err)
		}
		inputAudioPath = downloaded
		logger.Infof("Using downloaded YouTube audio: %s", inputAudioPath)
	}
	if *removeFatiha {
		processedPath, removed, err := maybeRemoveLeadingFatiha(ctx, inputAudioPath, cfg, logger)
		if err != nil {
			logger.Warnf("Al-Fatihah removal failed: %v", err)
		} else if removed {
			inputAudioPath = processedPath
		}
	}
	matcher := recognize.Matcher{
		Corpus:        newRecognizeCorpus(cfg.QuranAPI),
		ExpectedSurah: *expectedSurah,
	}
	audioForIdentify := inputAudioPath
	if cfg.Audio.EchoReduction {
		cleanDir := filepath.Join(cfg.Output.TempDir, "whisper_clean")
		if err := utils.EnsureDir(cleanDir); err == nil {
			cleanPath := filepath.Join(cleanDir, "identify_single_input.wav")
			if err := audio.EnhanceForSpeechRecognition(ctx, inputAudioPath, cleanPath, cfg.Audio.EchoFilter); err == nil {
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
