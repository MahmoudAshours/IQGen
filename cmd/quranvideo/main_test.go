package main

import (
	"testing"
	"time"

	"qgencodex/internal/quran"
	"qgencodex/internal/render"
)

func TestApplyAyahBoundariesFromWordTimings(t *testing.T) {
	timings := []render.Timing{
		{
			Verse: quran.Verse{Text: "a b"},
			Start: 0,
			End:   5 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "a", Start: 1 * time.Second, End: 2 * time.Second},
				{Word: "b", Start: 2 * time.Second, End: 4 * time.Second},
			},
		},
		{
			Verse: quran.Verse{Text: "c d"},
			Start: 5 * time.Second,
			End:   7 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "c", Start: 3 * time.Second, End: 4 * time.Second},
				{Word: "d", Start: 4 * time.Second, End: 6 * time.Second},
			},
		},
	}
	applyAyahBoundariesFromWordTimings(timings)
	if timings[0].Start != 1*time.Second || timings[0].End != 4*time.Second {
		t.Fatalf("unexpected boundaries for ayah 1: %v-%v", timings[0].Start, timings[0].End)
	}
	if timings[1].Start < timings[0].End {
		t.Fatalf("expected monotonic start for ayah 2")
	}
}

func TestEnsureContinuousTimings(t *testing.T) {
	timings := []render.Timing{
		{Verse: quran.Verse{Text: "a"}, Start: 0, End: 1 * time.Second},
		{Verse: quran.Verse{Text: "b"}, Start: 3 * time.Second, End: 4 * time.Second},
	}
	ensureContinuousTimings(timings, 5*time.Second)
	if timings[0].End != timings[1].Start {
		t.Fatalf("expected gap filled, got %v and %v", timings[0].End, timings[1].Start)
	}
	if timings[1].End != 5*time.Second {
		t.Fatalf("expected last end to match total")
	}
}
