package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"qgencodex/internal/align"
	"qgencodex/internal/audio"
	"qgencodex/internal/background"
	"qgencodex/internal/config"
	"qgencodex/internal/ffmpeg"
	"qgencodex/internal/quran"
	"qgencodex/internal/recognize"
	"qgencodex/internal/render"
	"qgencodex/internal/utils"
)

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

func maybeRemoveLeadingFatiha(ctx context.Context, inputAudioPath string, cfg *config.Config, logger *utils.Logger) (string, bool, error) {
	if strings.TrimSpace(inputAudioPath) == "" {
		return inputAudioPath, false, fmt.Errorf("audio path is required")
	}
	aligner := newIdentifyAligner(cfg.Audio)
	if !aligner.Available() {
		return inputAudioPath, false, fmt.Errorf("word timing backend unavailable")
	}
	words, err := aligner.TranscribeWordsContext(ctx, inputAudioPath, cfg.Audio.Language)
	if err != nil {
		return inputAudioPath, false, err
	}
	cutAt, summary, ok, err := detectLeadingFatihaCut(ctx, words, newRecognizeCorpus(cfg.QuranAPI))
	if err != nil {
		return inputAudioPath, false, err
	}
	if !ok {
		logger.Infof("Al-Fatihah removal: no leading Al-Fatihah detected")
		return inputAudioPath, false, nil
	}
	durSec, err := ffmpeg.ProbeDuration(ctx, inputAudioPath)
	if err != nil {
		return inputAudioPath, false, err
	}
	total := time.Duration(durSec * float64(time.Second))
	if total <= 0 {
		return inputAudioPath, false, nil
	}
	if cutAt > total {
		cutAt = total
	}
	if total-cutAt < 3*time.Second {
		logger.Warnf("Al-Fatihah removal skipped: remaining audio too short after cut (%s)", (total - cutAt).Round(time.Millisecond))
		return inputAudioPath, false, nil
	}
	if err := utils.EnsureDir(cfg.Output.TempDir); err != nil {
		return inputAudioPath, false, err
	}
	outPath := filepath.Join(cfg.Output.TempDir, "recitation_no_fatiha.mp3")
	if err := audio.TrimFrom(ctx, inputAudioPath, outPath, cutAt, cfg.Audio.BitrateKbps); err != nil {
		return inputAudioPath, false, err
	}
	logger.Infof("Removed leading Al-Fatihah/Ameen and pre-recitation gap: cut_at=%s matches=%d coverage=%.2f", cutAt.Round(time.Millisecond), summary.matches, summary.coverage)
	return outPath, true, nil
}

type fatihaDetectionSummary struct {
	matches  int
	coverage float64
}

func detectLeadingFatihaCut(ctx context.Context, words []align.WordTiming, corpus recognize.QuranCorpus) (time.Duration, fatihaDetectionSummary, bool, error) {
	if len(words) == 0 {
		return 0, fatihaDetectionSummary{}, false, nil
	}
	fatihaAyahs, err := corpus.FetchSurah(ctx, 1)
	if err != nil {
		return 0, fatihaDetectionSummary{}, false, err
	}
	if len(fatihaAyahs) < 7 {
		return 0, fatihaDetectionSummary{}, false, nil
	}
	target := make([]string, 0, 64)
	for i := 0; i < len(fatihaAyahs) && i < 7; i++ {
		for _, w := range strings.Fields(fatihaAyahs[i].Text) {
			n := normalizeAlignToken(w)
			if n != "" {
				target = append(target, n)
			}
		}
	}
	if len(target) == 0 {
		return 0, fatihaDetectionSummary{}, false, nil
	}
	spoken := make([]string, 0, len(words))
	spokenStarts := make([]time.Duration, 0, len(words))
	spokenEnds := make([]time.Duration, 0, len(words))
	for _, wt := range words {
		n := normalizeAlignToken(wt.Word)
		if n == "" {
			continue
		}
		spoken = append(spoken, n)
		spokenStarts = append(spokenStarts, wt.Start)
		spokenEnds = append(spokenEnds, wt.End)
	}
	if len(spoken) == 0 {
		return 0, fatihaDetectionSummary{}, false, nil
	}
	startIdx, endIdx, matches, _, _ := localAlignTokens(target, spoken)
	if startIdx < 0 || endIdx < startIdx || endIdx >= len(spokenEnds) {
		return 0, fatihaDetectionSummary{}, false, nil
	}
	coverage := float64(matches) / float64(len(target))
	summary := fatihaDetectionSummary{matches: matches, coverage: coverage}
	if startIdx > 8 {
		return 0, summary, false, nil
	}
	minMatches := max(8, len(target)/3)
	if matches < minMatches || coverage < 0.35 {
		return 0, summary, false, nil
	}
	// Include "Ameen" tokens right after Al-Fatihah in the removed section.
	removalEndIdx := endIdx
	for removalEndIdx+1 < len(spoken) && isAmeenToken(spoken[removalEndIdx+1]) {
		removalEndIdx++
	}
	// Start from the next recitation word to also remove breathing/pause
	// between the removed prefix and actual recitation start.
	cut := spokenEnds[removalEndIdx]
	if removalEndIdx+1 < len(spokenStarts) {
		nextStart := spokenStarts[removalEndIdx+1]
		if nextStart > 0 {
			cut = nextStart
		}
	}
	if cut <= 0 {
		return 0, summary, false, nil
	}
	return cut, summary, true, nil
}

func isAmeenToken(token string) bool {
	t := strings.TrimSpace(strings.ToLower(token))
	switch t {
	case "امين", "amen", "ameen", "amin":
		return true
	default:
		return false
	}
}

func normalizeAlignToken(word string) string {
	base := align.NormalizeWord(word)
	if base == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range base {
		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			b.WriteRune('ا')
		case 'ى':
			b.WriteRune('ي')
		case 'ؤ':
			b.WriteRune('و')
		case 'ئ':
			b.WriteRune('ي')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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

func isCaptionsOnlyMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "caption", "captions", "captions-only", "srt", "subtitle", "subtitles":
		return true
	default:
		return false
	}
}

func forceSRTExt(path string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	if strings.EqualFold(filepath.Ext(path), ".srt") {
		return path
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".srt"
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
