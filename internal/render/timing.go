package render

import (
	"fmt"
	"strings"
	"time"

	"qgencodex/internal/audio"
	"qgencodex/internal/quran"
)

type Timing struct {
	Verse       quran.Verse
	Start       time.Duration
	End         time.Duration
	WordTimings []WordTiming
}

type WordTiming struct {
	Word  string
	Start time.Duration
	End   time.Duration
}

// BuildTimings maps verses and audio segments to timeline timings.
func BuildTimings(verses []quran.Verse, segments []audio.Segment) ([]Timing, error) {
	if len(verses) != len(segments) {
		return nil, fmt.Errorf("verses and audio segments count mismatch")
	}
	timings := make([]Timing, len(verses))
	cursor := time.Duration(0)
	for i, verse := range verses {
		seg := segments[i]
		start := cursor
		end := cursor + seg.Duration
		cursor = end
		words := strings.Fields(verse.Text)
		var wordTimings []WordTiming
		if len(words) > 0 {
			perWord := seg.Duration / time.Duration(len(words))
			wc := start
			for idx, w := range words {
				we := wc + perWord
				if idx == len(words)-1 {
					we = end
				}
				wordTimings = append(wordTimings, WordTiming{Word: w, Start: wc, End: we})
				wc = we
			}
		}
		timings[i] = Timing{
			Verse:       verse,
			Start:       start,
			End:         end,
			WordTimings: wordTimings,
		}
	}
	return timings, nil
}
