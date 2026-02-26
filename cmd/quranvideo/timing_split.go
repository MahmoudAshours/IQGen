package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"qgencodex/internal/align"
	"qgencodex/internal/audio"
	"qgencodex/internal/config"
	"qgencodex/internal/quran"
	"qgencodex/internal/render"
	"qgencodex/internal/utils"
)

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
