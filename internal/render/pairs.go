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

const minTwoWordPairDuration = 300 * time.Millisecond

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
	return smoothPairDurations(pairs, minTwoWordPairDuration)
}

func smoothPairDurations(in []wordPair, minDur time.Duration) []wordPair {
	if len(in) == 0 || minDur <= 0 {
		return in
	}
	out := make([]wordPair, len(in))
	copy(out, in)
	if len(out) == 1 {
		if out[0].End < out[0].Start {
			out[0].End = out[0].Start
		}
		return out
	}
	start := out[0].Start
	finalEnd := out[len(out)-1].End
	if finalEnd <= start {
		return out
	}
	total := finalEnd - start
	durs := make([]time.Duration, len(out))
	sum := time.Duration(0)
	for i := range out {
		d := out[i].End - out[i].Start
		if d < 0 {
			d = 0
		}
		durs[i] = d
		sum += d
	}
	if sum <= 0 {
		return out
	}
	// Preserve overall timing span.
	scale := float64(total) / float64(sum)
	scaled := make([]time.Duration, len(durs))
	scaledSum := time.Duration(0)
	for i, d := range durs {
		scaled[i] = time.Duration(float64(d) * scale)
		scaledSum += scaled[i]
	}
	if diff := total - scaledSum; diff != 0 {
		scaled[len(scaled)-1] += diff
	}
	durs = scaled
	minEff := minDuration(minDur, total/time.Duration(len(durs)))
	if minEff > 0 {
		for i := range durs {
			if durs[i] >= minEff {
				continue
			}
			need := minEff - durs[i]
			// Borrow from later/longer pairs first to minimize shifting early content.
			for j := len(durs) - 1; j >= 0 && need > 0; j-- {
				if j == i {
					continue
				}
				avail := durs[j] - minEff
				if avail <= 0 {
					continue
				}
				give := minDuration(need, avail)
				durs[j] -= give
				durs[i] += give
				need -= give
			}
		}
	}
	cursor := start
	for i := range out {
		out[i].Start = cursor
		end := cursor + durs[i]
		if i == len(out)-1 || end > finalEnd {
			end = finalEnd
		}
		if end < out[i].Start {
			end = out[i].Start
		}
		out[i].End = end
		cursor = end
	}
	return out
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
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
