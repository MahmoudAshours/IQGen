package render

import (
	"testing"
	"time"

	"qgencodex/internal/quran"
)

func TestBuildWordPairs_SmoothsVeryShortPairsWithoutEvenFallback(t *testing.T) {
	timing := Timing{
		Verse: quran.Verse{Text: "وَلَمَّا رَأَى الْمُؤْمِنُونَ الْأَحْزَابَ قَالُوا هَذَا"},
		Start: 20 * time.Second,
		End:   24 * time.Second,
		WordTimings: []WordTiming{
			{Word: "وَلَمَّا", Start: 20*time.Second + 420*time.Millisecond, End: 20*time.Second + 520*time.Millisecond},
			{Word: "رَأَى", Start: 20*time.Second + 520*time.Millisecond, End: 20*time.Second + 620*time.Millisecond},
			{Word: "الْمُؤْمِنُونَ", Start: 20*time.Second + 620*time.Millisecond, End: 20*time.Second + 720*time.Millisecond},
			{Word: "الْأَحْزَابَ", Start: 20*time.Second + 720*time.Millisecond, End: 20*time.Second + 820*time.Millisecond},
			{Word: "قَالُوا", Start: 20*time.Second + 820*time.Millisecond, End: 22 * time.Second},
			{Word: "هَذَا", Start: 22 * time.Second, End: 24 * time.Second},
		},
	}

	pairs := buildWordPairs(timing)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}
	// Short flashes should be smoothed above the minimum threshold.
	for i, p := range pairs[:2] {
		if p.End-p.Start < minTwoWordPairDuration {
			t.Fatalf("pair %d still too short: %s", i, p.End-p.Start)
		}
	}
	// Keep global order and avoid fully-even repartition.
	if pairs[2].Start >= 22*time.Second {
		t.Fatalf("unexpected strong delay from smoothing: third pair starts at %s", pairs[2].Start)
	}
}
