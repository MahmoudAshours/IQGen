package render

import (
	"strings"
	"time"
)

type wordPair struct {
	Text  string
	Start time.Duration
	End   time.Duration
}

func buildWordPairs(t Timing) []wordPair {
	if len(t.WordTimings) == 0 {
		return buildEvenPairsFromText(t.Verse.Text, t.Start, t.End)
	}
	words := make([]string, len(t.WordTimings))
	for i, w := range t.WordTimings {
		words[i] = w.Word
	}
	pairs := make([]wordPair, 0, (len(words)+1)/2)
	invalid := false
	for i := 0; i < len(t.WordTimings); i += 2 {
		first := t.WordTimings[i]
		text := strings.TrimSpace(first.Word)
		start := first.Start
		end := first.End
		if i+1 < len(t.WordTimings) {
			second := t.WordTimings[i+1]
			if strings.TrimSpace(second.Word) != "" {
				text = strings.TrimSpace(text + " " + second.Word)
			}
			if second.End > end {
				end = second.End
			}
		}
		if end <= start || text == "" {
			invalid = true
		}
		pairs = append(pairs, wordPair{Text: text, Start: start, End: end})
	}
	if invalid {
		return buildEvenPairs(words, t.Start, t.End)
	}
	return pairs
}

func buildEvenPairsFromText(text string, start, end time.Duration) []wordPair {
	words := strings.Fields(text)
	return buildEvenPairs(words, start, end)
}

func buildEvenPairs(words []string, start, end time.Duration) []wordPair {
	if len(words) == 0 || end <= start {
		return nil
	}
	pairCount := (len(words) + 1) / 2
	if pairCount <= 0 {
		return nil
	}
	total := end - start
	per := total / time.Duration(pairCount)
	if per <= 0 {
		return nil
	}
	pairs := make([]wordPair, 0, pairCount)
	cursor := start
	for i := 0; i < len(words); i += 2 {
		text := words[i]
		if i+1 < len(words) {
			text = text + " " + words[i+1]
		}
		s := cursor
		e := s + per
		if len(pairs) == pairCount-1 {
			e = end
		}
		pairs = append(pairs, wordPair{Text: text, Start: s, End: e})
		cursor = e
	}
	return pairs
}
