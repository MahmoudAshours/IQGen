package align

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type WhisperAligner struct {
	Cmd string
}

func NewWhisperAligner(cmd string) *WhisperAligner {
	if cmd == "" {
		cmd = "whisper"
	}
	return &WhisperAligner{Cmd: cmd}
}

func (w *WhisperAligner) Available() bool {
	_, err := exec.LookPath(w.Cmd)
	return err == nil
}

func (w *WhisperAligner) Align(audioPath string, words []string, language string) ([]WordTiming, error) {
	if !w.Available() {
		return nil, fmt.Errorf("whisper command not found: %s", w.Cmd)
	}
	if len(words) == 0 {
		return nil, errors.New("no words to align")
	}
	if language == "" {
		language = "ar"
	}
	outputDir, err := os.MkdirTemp("", "quranvideo-whisper-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(outputDir)

	jsonPath := filepath.Join(outputDir, strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))+".json")
	prompt := strings.TrimSpace(strings.Join(words, " "))
	baseArgs := []string{
		"--language", language,
		"--word_timestamps", "True",
		"--output_format", "json",
		"--output_dir", outputDir,
		"--task", "transcribe",
		audioPath,
	}
	advancedArgs := append([]string{}, baseArgs...)
	advancedArgs = append(advancedArgs,
		"--temperature", "0",
		"--beam_size", "5",
		"--best_of", "5",
	)
	if prompt != "" {
		advancedArgs = append(advancedArgs, "--initial_prompt", prompt)
	}
	if err := runWhisper(w.Cmd, advancedArgs); err != nil {
		_ = os.Remove(jsonPath)
		if err := runWhisper(w.Cmd, baseArgs); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, err
	}
	var result whisperResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	whisperWords := flattenWhisperWords(result)
	if len(whisperWords) == 0 {
		return nil, errors.New("no words found in whisper output")
	}
	var (
		aligned []WordTiming
		ok      bool
	)
	units := buildAlignUnits(words)
	unitTimings, ok := alignUnitsToWhisper(units, whisperWords)
	if ok {
		return expandUnitsToWords(units, unitTimings, words), nil
	}
	aligned, ok = alignByNormalizedMatch(words, whisperWords)
	if ok {
		return aligned, nil
	}
	aligned, ok = alignByLCS(words, whisperWords)
	if ok {
		return aligned, nil
	}
	aligned, ok = alignByIndexStrict(words, whisperWords)
	if ok {
		return aligned, nil
	}
	return nil, fmt.Errorf("unable to align %d words with %d whisper words", len(words), len(whisperWords))
}

type whisperResult struct {
	Segments []whisperSegment `json:"segments"`
}

type whisperSegment struct {
	Words []whisperWord `json:"words"`
}

type whisperWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type flatWord struct {
	Word  string
	Start time.Duration
	End   time.Duration
}

func flattenWhisperWords(result whisperResult) []flatWord {
	var out []flatWord
	for _, seg := range result.Segments {
		for _, w := range seg.Words {
			out = append(out, flatWord{
				Word:  strings.TrimSpace(w.Word),
				Start: time.Duration(w.Start * float64(time.Second)),
				End:   time.Duration(w.End * float64(time.Second)),
			})
		}
	}
	return out
}

type alignUnit struct {
	Match   string
	Indices []int
}

type unitTiming struct {
	Start   time.Duration
	End     time.Duration
	Matched bool
}

func buildAlignUnits(words []string) []alignUnit {
	if len(words) == 0 {
		return nil
	}
	units := make([]alignUnit, 0, len(words))
	for i := 0; i < len(words); i++ {
		word := words[i]
		norm := normalizeForMatch(word)
		if isClitic(norm) && i+1 < len(words) {
			next := words[i+1]
			combined := normalizeForMatch(word + next)
			units = append(units, alignUnit{
				Match:   combined,
				Indices: []int{i, i + 1},
			})
			i++
			continue
		}
		units = append(units, alignUnit{
			Match:   norm,
			Indices: []int{i},
		})
	}
	return units
}

func alignUnitsToWhisper(units []alignUnit, whisperWords []flatWord) ([]unitTiming, bool) {
	if len(units) == 0 || len(whisperWords) == 0 {
		return nil, false
	}
	normUnits := make([]string, len(units))
	for i, u := range units {
		normUnits[i] = u.Match
	}
	normWhisper := make([]string, len(whisperWords))
	for i, w := range whisperWords {
		normWhisper[i] = normalizeForMatch(w.Word)
	}
	matches := lcsMatches(normUnits, normWhisper)
	if len(matches) == 0 {
		return nil, false
	}
	matchRatio := float64(len(matches)) / float64(len(units))
	if matchRatio < 0.25 && len(matches) < 2 {
		return nil, false
	}
	timings := make([]unitTiming, len(units))
	for _, m := range matches {
		timings[m.i] = unitTiming{
			Start:   whisperWords[m.j].Start,
			End:     whisperWords[m.j].End,
			Matched: true,
		}
	}
	fillMissingUnitTimings(timings, whisperWords)
	return timings, true
}

type lcsPair struct{ i, j int }

func lcsMatches(a, b []string) []lcsPair {
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
	matches := make([]lcsPair, 0, dp[n][m])
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] != "" && a[i-1] == b[j-1] {
			matches = append(matches, lcsPair{i - 1, j - 1})
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	for l, r := 0, len(matches)-1; l < r; l, r = l+1, r-1 {
		matches[l], matches[r] = matches[r], matches[l]
	}
	return matches
}

func fillMissingUnitTimings(timings []unitTiming, whisperWords []flatWord) {
	if len(timings) == 0 || len(whisperWords) == 0 {
		return
	}
	firstStart := whisperWords[0].Start
	lastEnd := whisperWords[len(whisperWords)-1].End
	prevIdx := -1
	for i := 0; i < len(timings); i++ {
		if !timings[i].Matched {
			continue
		}
		if prevIdx+1 < i {
			fillUnitGap(timings, prevIdx, i, firstStart, lastEnd)
		}
		prevIdx = i
	}
	if prevIdx < len(timings)-1 {
		fillUnitGap(timings, prevIdx, len(timings), firstStart, lastEnd)
	}
}

func fillUnitGap(timings []unitTiming, prevIdx, nextIdx int, firstStart, lastEnd time.Duration) {
	start := firstStart
	if prevIdx >= 0 {
		start = timings[prevIdx].End
	}
	end := lastEnd
	if nextIdx < len(timings) {
		end = timings[nextIdx].Start
	}
	if end < start {
		end = start
	}
	count := nextIdx - prevIdx - 1
	if count <= 0 {
		return
	}
	step := time.Duration(0)
	if end > start {
		step = (end - start) / time.Duration(count+1)
	}
	cursor := start
	for i := prevIdx + 1; i < nextIdx; i++ {
		s := cursor + step
		e := s + step
		if step == 0 {
			e = s
		}
		timings[i] = unitTiming{Start: s, End: e, Matched: true}
		cursor = e
	}
}

func expandUnitsToWords(units []alignUnit, unitTimings []unitTiming, words []string) []WordTiming {
	result := make([]WordTiming, len(words))
	assigned := make([]bool, len(words))
	for i := range words {
		result[i].Word = words[i]
	}
	for idx, unit := range units {
		if idx >= len(unitTimings) {
			break
		}
		ut := unitTimings[idx]
		if ut.End < ut.Start {
			ut.End = ut.Start
		}
		if len(unit.Indices) == 1 {
			wordIdx := unit.Indices[0]
			if wordIdx >= 0 && wordIdx < len(result) {
				result[wordIdx].Start = ut.Start
				result[wordIdx].End = ut.End
				assigned[wordIdx] = true
			}
			continue
		}
		weights := make([]int, len(unit.Indices))
		total := 0
		for i, wordIdx := range unit.Indices {
			if wordIdx >= 0 && wordIdx < len(words) {
				weights[i] = wordLen(words[wordIdx])
			}
			if weights[i] <= 0 {
				weights[i] = 1
			}
			total += weights[i]
		}
		if total == 0 {
			total = len(unit.Indices)
		}
		duration := ut.End - ut.Start
		cursor := ut.Start
		for i, wordIdx := range unit.Indices {
			share := time.Duration(0)
			if i == len(unit.Indices)-1 {
				share = ut.End - cursor
			} else {
				share = time.Duration(int64(duration) * int64(weights[i]) / int64(total))
			}
			if share < 0 {
				share = 0
			}
			start := cursor
			end := start + share
			if end < start {
				end = start
			}
			if wordIdx >= 0 && wordIdx < len(result) {
				result[wordIdx].Start = start
				result[wordIdx].End = end
				assigned[wordIdx] = true
			}
			cursor = end
		}
	}
	fillMissingWordTimings(result, assigned)
	return result
}

func fillMissingWordTimings(timings []WordTiming, assigned []bool) {
	if len(timings) == 0 {
		return
	}
	firstStart, lastEnd := findTimingBounds(timings, assigned)
	if lastEnd <= firstStart {
		return
	}
	prevIdx := -1
	for i := 0; i < len(timings); i++ {
		if !assigned[i] {
			continue
		}
		if prevIdx+1 < i {
			fillWordGap(timings, assigned, prevIdx, i, firstStart, lastEnd)
		}
		prevIdx = i
	}
	if prevIdx < len(timings)-1 {
		fillWordGap(timings, assigned, prevIdx, len(timings), firstStart, lastEnd)
	}
}

func findTimingBounds(timings []WordTiming, assigned []bool) (time.Duration, time.Duration) {
	first := time.Duration(0)
	last := time.Duration(0)
	found := false
	for i, t := range timings {
		if assigned != nil && len(assigned) == len(timings) && !assigned[i] {
			continue
		}
		if !found || t.Start < first {
			first = t.Start
		}
		if t.End > last {
			last = t.End
		}
		found = true
	}
	return first, last
}

func fillWordGap(timings []WordTiming, assigned []bool, prevIdx, nextIdx int, firstStart, lastEnd time.Duration) {
	start := firstStart
	if prevIdx >= 0 {
		start = timings[prevIdx].End
	}
	end := lastEnd
	if nextIdx < len(timings) {
		end = timings[nextIdx].Start
	}
	if end < start {
		end = start
	}
	count := nextIdx - prevIdx - 1
	if count <= 0 {
		return
	}
	step := time.Duration(0)
	if end > start {
		step = (end - start) / time.Duration(count+1)
	}
	cursor := start
	for i := prevIdx + 1; i < nextIdx; i++ {
		s := cursor + step
		e := s + step
		if step == 0 {
			e = s
		}
		timings[i].Start = s
		timings[i].End = e
		if assigned != nil && len(assigned) == len(timings) {
			assigned[i] = true
		}
		cursor = e
	}
}

func normalizeForMatch(word string) string {
	return strings.ReplaceAll(normalizeWord(word), " ", "")
}

func isClitic(norm string) bool {
	if len(norm) != 1 {
		return false
	}
	switch norm {
	case "و", "ف", "ب", "ل", "ك", "س":
		return true
	default:
		return false
	}
}

func wordLen(word string) int {
	count := 0
	for _, r := range strings.TrimSpace(word) {
		if r == 'ـ' {
			continue
		}
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

func alignByNormalizedMatch(words []string, whisperWords []flatWord) ([]WordTiming, bool) {
	aligned := make([]WordTiming, 0, len(words))
	idx := 0
	for _, w := range words {
		needle := normalizeWord(w)
		found := false
		for idx < len(whisperWords) {
			candidate := normalizeWord(whisperWords[idx].Word)
			if needle != "" && needle == candidate {
				aligned = append(aligned, WordTiming{Word: w, Start: whisperWords[idx].Start, End: whisperWords[idx].End})
				idx++
				found = true
				break
			}
			idx++
		}
		if !found {
			return nil, false
		}
	}
	return aligned, len(aligned) == len(words)
}

func alignByLCS(words []string, whisperWords []flatWord) ([]WordTiming, bool) {
	if len(words) == 0 || len(whisperWords) == 0 {
		return nil, false
	}
	normWords := make([]string, len(words))
	for i, w := range words {
		normWords[i] = normalizeWord(w)
	}
	normWhisper := make([]string, len(whisperWords))
	for i, w := range whisperWords {
		normWhisper[i] = normalizeWord(w.Word)
	}
	n, m := len(normWords), len(normWhisper)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if normWords[i-1] != "" && normWords[i-1] == normWhisper[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	matchCount := dp[n][m]
	if matchCount == 0 {
		return nil, false
	}
	matchRatio := float64(matchCount) / float64(len(words))
	if matchRatio < 0.45 {
		return nil, false
	}
	type pair struct{ i, j int }
	matches := make([]pair, 0, matchCount)
	i, j := n, m
	for i > 0 && j > 0 {
		if normWords[i-1] != "" && normWords[i-1] == normWhisper[j-1] {
			matches = append(matches, pair{i - 1, j - 1})
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	for l, r := 0, len(matches)-1; l < r; l, r = l+1, r-1 {
		matches[l], matches[r] = matches[r], matches[l]
	}
	timings := make([]WordTiming, len(words))
	matched := make([]bool, len(words))
	for i := range words {
		timings[i].Word = words[i]
	}
	for _, p := range matches {
		timings[p.i] = WordTiming{Word: words[p.i], Start: whisperWords[p.j].Start, End: whisperWords[p.j].End}
		matched[p.i] = true
	}
	fillMissingTimings(timings, matched, whisperWords)
	return timings, true
}

func alignByIndexStrict(words []string, whisperWords []flatWord) ([]WordTiming, bool) {
	if len(words) == 0 {
		return nil, false
	}
	if len(whisperWords) != len(words) {
		return nil, false
	}
	matches := 0
	for i, w := range words {
		if normalizeWord(w) == normalizeWord(whisperWords[i].Word) {
			matches++
		}
	}
	if float64(matches)/float64(len(words)) < 0.5 {
		return nil, false
	}
	aligned := make([]WordTiming, 0, len(words))
	for i, w := range words {
		ww := whisperWords[i]
		aligned = append(aligned, WordTiming{Word: w, Start: ww.Start, End: ww.End})
	}
	return aligned, true
}

func fillMissingTimings(timings []WordTiming, matched []bool, whisperWords []flatWord) {
	if len(timings) == 0 || len(whisperWords) == 0 {
		return
	}
	firstStart := whisperWords[0].Start
	lastEnd := whisperWords[len(whisperWords)-1].End
	prevIdx := -1
	for i := 0; i < len(timings); i++ {
		if !matched[i] {
			continue
		}
		if prevIdx+1 < i {
			fillGap(timings, matched, prevIdx, i, firstStart, lastEnd)
		}
		prevIdx = i
	}
	if prevIdx < len(timings)-1 {
		fillGap(timings, matched, prevIdx, len(timings), firstStart, lastEnd)
	}
}

func fillGap(timings []WordTiming, matched []bool, prevIdx, nextIdx int, firstStart, lastEnd time.Duration) {
	start := firstStart
	if prevIdx >= 0 {
		start = timings[prevIdx].End
	}
	end := lastEnd
	if nextIdx < len(timings) {
		end = timings[nextIdx].Start
	}
	if end < start {
		end = start
	}
	count := nextIdx - prevIdx - 1
	if count <= 0 {
		return
	}
	step := time.Duration(0)
	if end > start {
		step = (end - start) / time.Duration(count+1)
	}
	cursor := start
	for i := prevIdx + 1; i < nextIdx; i++ {
		s := cursor + step
		e := s + step
		if step == 0 {
			e = s
		}
		timings[i] = WordTiming{Word: timings[i].Word, Start: s, End: e}
		matched[i] = true
		cursor = e
	}
}

func normalizeWord(word string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(word) {
		if r == 'ـ' {
			continue
		}
		if unicode.Is(unicode.Mn, r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func runWhisper(cmd string, args []string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
