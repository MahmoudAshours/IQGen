package recognize

import (
	"context"
	"errors"
	"strings"
	"unicode"
)

type Matcher struct {
	Corpus QuranCorpus
	// ExpectedSurah optionally narrows search to a single surah (1-114).
	ExpectedSurah int
}

func (m *Matcher) Identify(ctx context.Context, text string) (Result, error) {
	if m.Corpus == nil {
		return Result{}, errors.New("corpus is nil")
	}
	needles := normalizeTokens(text)
	if len(needles) == 0 {
		return Result{}, errors.New("empty normalized text")
	}
	best := Result{}
	bestMatches := 0
	bestCoverage := 0.0
	bestScore := 0
	bestLen := 0
	startSurah := 1
	endSurah := 114
	if m.ExpectedSurah >= 1 && m.ExpectedSurah <= 114 {
		startSurah = m.ExpectedSurah
		endSurah = m.ExpectedSurah
	}
	for surah := startSurah; surah <= endSurah; surah++ {
		ayahs, err := m.Corpus.FetchSurah(ctx, surah)
		if err != nil {
			continue
		}
		candidate, ok := matchSurah(needles, ayahs)
		if !ok {
			continue
		}
		if candidate.Matches > bestMatches ||
			(candidate.Matches == bestMatches && candidate.Coverage > bestCoverage) ||
			(candidate.Matches == bestMatches && candidate.Coverage == bestCoverage && candidate.Score > bestScore) ||
			(candidate.Matches == bestMatches && candidate.Coverage == bestCoverage && candidate.Score == bestScore && candidate.Length > bestLen) {
			bestMatches = candidate.Matches
			bestCoverage = candidate.Coverage
			bestScore = candidate.Score
			bestLen = candidate.Length
			best = Result{Surah: surah, StartAyah: candidate.StartAyah, EndAyah: candidate.EndAyah}
		}
	}
	minMatches := minRequiredMatches(len(needles))
	if bestMatches < minMatches || bestCoverage < minCoverage(len(needles)) {
		return Result{}, errors.New("no reliable match found")
	}
	if bestScore == 0 {
		return Result{}, errors.New("no match found")
	}
	return best, nil
}

type matchCandidate struct {
	StartAyah int
	EndAyah   int
	Matches   int
	Length    int
	Score     int
	Coverage  float64
}

func matchSurah(needles []string, ayahs []Ayah) (matchCandidate, bool) {
	tokens, tokenAyah := flattenAyahTokens(ayahs)
	if len(tokens) == 0 || len(tokenAyah) == 0 {
		return matchCandidate{}, false
	}
	startIdx, endIdx, matches, length, score := localAlign(needles, tokens)
	if matches == 0 || endIdx < 0 || startIdx < 0 || startIdx >= len(tokenAyah) || endIdx >= len(tokenAyah) {
		return matchCandidate{}, false
	}
	if endIdx < startIdx {
		startIdx, endIdx = endIdx, startIdx
	}
	startAyah := tokenAyah[startIdx]
	endAyah := tokenAyah[endIdx]
	if startAyah == 0 || endAyah == 0 {
		return matchCandidate{}, false
	}
	return matchCandidate{
		StartAyah: startAyah,
		EndAyah:   endAyah,
		Matches:   matches,
		Length:    length,
		Score:     score,
		Coverage:  float64(matches) / float64(len(needles)),
	}, true
}

func flattenAyahTokens(ayahs []Ayah) ([]string, []int) {
	tokens := make([]string, 0, len(ayahs)*10)
	tokenAyah := make([]int, 0, len(ayahs)*10)
	for i, ayah := range ayahs {
		ayahTokens := normalizeTokens(ayah.Text)
		for _, tok := range ayahTokens {
			tokens = append(tokens, tok)
			tokenAyah = append(tokenAyah, i+1)
		}
	}
	return tokens, tokenAyah
}

type alignCell struct {
	score   int
	start   int
	matches int
	length  int
}

func localAlign(needles []string, haystack []string) (startIdx, endIdx, matches, length, score int) {
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

func minRequiredMatches(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	if tokens < 6 {
		return tokens
	}
	if tokens < 12 {
		return 3
	}
	if tokens < 20 {
		return 4
	}
	return tokens / 4
}

func minCoverage(tokens int) float64 {
	if tokens < 10 {
		return 0.5
	}
	if tokens < 30 {
		return 0.4
	}
	return 0.35
}

func normalize(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == 'ـ' {
			continue
		}
		if unicode.Is(unicode.Mn, r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(unicode.ToLower(normalizeRune(r)))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func normalizeTokens(text string) []string {
	normalized := normalize(text)
	if normalized == "" {
		return nil
	}
	return strings.Fields(normalized)
}

func normalizeRune(r rune) rune {
	switch r {
	case 'أ', 'إ', 'آ', 'ٱ':
		return 'ا'
	case 'ى':
		return 'ي'
	case 'ؤ':
		return 'و'
	case 'ئ':
		return 'ي'
	default:
		return r
	}
}
