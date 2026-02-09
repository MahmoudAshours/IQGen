package render

import (
	"testing"
	"time"

	"qgencodex/internal/audio"
	"qgencodex/internal/quran"
)

func TestBuildTimings(t *testing.T) {
	verses := []quran.Verse{
		{Text: "hello world"},
		{Text: "foo bar"},
	}
	segments := []audio.Segment{
		{Duration: 2 * time.Second},
		{Duration: 3 * time.Second},
	}
	timings, err := BuildTimings(verses, segments)
	if err != nil {
		t.Fatalf("BuildTimings failed: %v", err)
	}
	if len(timings) != 2 {
		t.Fatalf("expected 2 timings")
	}
	if timings[0].Start != 0 || timings[0].End != 2*time.Second {
		t.Fatalf("unexpected timing for verse 1")
	}
	if len(timings[0].WordTimings) != 2 {
		t.Fatalf("expected word timings")
	}
	if timings[0].WordTimings[0].End != 1*time.Second {
		t.Fatalf("expected evenly split word timing")
	}
	if timings[1].Start != 2*time.Second || timings[1].End != 5*time.Second {
		t.Fatalf("unexpected timing for verse 2")
	}
}
