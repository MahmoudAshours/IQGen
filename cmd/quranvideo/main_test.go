package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qgencodex/internal/align"
	"qgencodex/internal/config"
	"qgencodex/internal/quran"
	"qgencodex/internal/recognize"
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

func TestSTTHelpers(t *testing.T) {
	cfg := config.Default().Audio
	cfg.STTBackend = ""
	cfg.WhisperCmd = ""
	cfg.STTTimeoutSec = 0
	if got := sttBackendLabel(cfg); got != "auto" {
		t.Fatalf("unexpected backend label: %s", got)
	}
	if got := sttCommandLabel(cfg); got != "whisper" {
		t.Fatalf("unexpected command label: %s", got)
	}
	if got := sttTimeout(cfg); got != 90*time.Second {
		t.Fatalf("unexpected timeout: %s", got)
	}
}

func TestTranscriptFromWordTimings(t *testing.T) {
	words := []align.WordTiming{
		{Word: "  "},
		{Word: "بسم"},
		{Word: "الله"},
	}
	got := transcriptFromWordTimings(words)
	if got != "بسم الله" {
		t.Fatalf("unexpected transcript: %q", got)
	}
}

func TestNormalizeWordTimings_AppendsStandaloneMarks(t *testing.T) {
	timings := []render.Timing{
		{
			Verse: quran.Verse{Text: "dummy"},
			Start: 0,
			End:   2 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "وَٱغْلُظْ", Start: 0, End: 800 * time.Millisecond},
				{Word: "ۖ", Start: 800 * time.Millisecond, End: 900 * time.Millisecond},
				{Word: "عَلَيْهِمْ", Start: 900 * time.Millisecond, End: 1500 * time.Millisecond},
			},
		},
	}

	normalizeWordTimings(timings)

	got := timings[0].WordTimings
	if len(got) != 2 {
		t.Fatalf("expected 2 dialogue words after mark merge, got %d", len(got))
	}
	if got[0].Word != "وَٱغْلُظْۖ" {
		t.Fatalf("expected mark to append to previous word, got %q", got[0].Word)
	}
	if isStandaloneMark(got[0].Word) || isStandaloneMark(got[1].Word) {
		t.Fatalf("unexpected standalone mark word after normalization: %#v", got)
	}
}

func TestSplitLineIntoAyahParts(t *testing.T) {
	parts, next := splitLineIntoAyahParts("الرَّحْمَنِ الرَّحِيمِ ( 3 ) مَالِكِ يَوْمِ الدِّينِ ( 4 )", 3)
	if next != 5 {
		t.Fatalf("expected next ayah 5, got %d", next)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Ayah != 3 || parts[1].Ayah != 4 {
		t.Fatalf("unexpected part ayahs: %+v", parts)
	}
}

func TestBuildLineModeTimings(t *testing.T) {
	dir := t.TempDir()
	lines := "بِسْمِ اللَّهِ الرَّحْمَنِ الرَّحِيمِ ( 1 )\nالْحَمْدُ لِلَّهِ رَبِّ الْعَالَمِينَ ( 2 )\n"
	if err := os.WriteFile(filepath.Join(dir, "1.txt"), []byte(lines), 0o644); err != nil {
		t.Fatalf("write lines file: %v", err)
	}
	t.Setenv("QURAN_LINES_DIR", dir)
	timings := []render.Timing{
		{
			Verse: quran.Verse{NumberInSurah: 1, Text: "بِسْمِ اللَّهِ الرَّحْمَنِ الرَّحِيمِ", Translation: "In the name of Allah"},
			Start: 0,
			End:   4 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "بِسْمِ", Start: 0, End: 1 * time.Second},
				{Word: "اللَّهِ", Start: 1 * time.Second, End: 2 * time.Second},
				{Word: "الرَّحْمَنِ", Start: 2 * time.Second, End: 3 * time.Second},
				{Word: "الرَّحِيمِ", Start: 3 * time.Second, End: 4 * time.Second},
			},
		},
		{
			Verse: quran.Verse{NumberInSurah: 2, Text: "الْحَمْدُ لِلَّهِ رَبِّ الْعَالَمِينَ", Translation: "All praise is due to Allah"},
			Start: 4 * time.Second,
			End:   8 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "الْحَمْدُ", Start: 4 * time.Second, End: 5 * time.Second},
				{Word: "لِلَّهِ", Start: 5 * time.Second, End: 6 * time.Second},
				{Word: "رَبِّ", Start: 6 * time.Second, End: 7 * time.Second},
				{Word: "الْعَالَمِينَ", Start: 7 * time.Second, End: 8 * time.Second},
			},
		},
	}
	got, err := buildLineModeTimings(1, 1, 2, timings)
	if err != nil {
		t.Fatalf("buildLineModeTimings failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 line timings, got %d", len(got))
	}
	if got[0].Start != 0 || got[0].End != 4*time.Second {
		t.Fatalf("unexpected first line timing: %v-%v", got[0].Start, got[0].End)
	}
	if got[1].Start != 4*time.Second || got[1].End != 8*time.Second {
		t.Fatalf("unexpected second line timing: %v-%v", got[1].Start, got[1].End)
	}
}

type fakeCorpus struct {
	surah map[int][]recognize.Ayah
}

func (f *fakeCorpus) FetchSurah(_ context.Context, surah int) ([]recognize.Ayah, error) {
	if out, ok := f.surah[surah]; ok {
		return out, nil
	}
	return nil, nil
}

func TestDetectLeadingFatihaCut(t *testing.T) {
	corpus := &fakeCorpus{
		surah: map[int][]recognize.Ayah{
			1: {
				{NumberInSurah: 1, Text: "بِسْمِ اللَّهِ الرَّحْمَٰنِ الرَّحِيمِ"},
				{NumberInSurah: 2, Text: "الْحَمْدُ لِلَّهِ رَبِّ الْعَالَمِينَ"},
				{NumberInSurah: 3, Text: "الرَّحْمَٰنِ الرَّحِيمِ"},
				{NumberInSurah: 4, Text: "مَالِكِ يَوْمِ الدِّينِ"},
				{NumberInSurah: 5, Text: "إِيَّاكَ نَعْبُدُ وَإِيَّاكَ نَسْتَعِينُ"},
				{NumberInSurah: 6, Text: "اهْدِنَا الصِّرَاطَ الْمُسْتَقِيمَ"},
				{NumberInSurah: 7, Text: "صِرَاطَ الَّذِينَ أَنْعَمْتَ عَلَيْهِمْ"},
			},
		},
	}
	words := []align.WordTiming{
		{Word: "بسم", Start: 0 * time.Second, End: 1 * time.Second},
		{Word: "الله", Start: 1 * time.Second, End: 2 * time.Second},
		{Word: "الرحمن", Start: 2 * time.Second, End: 3 * time.Second},
		{Word: "الرحيم", Start: 3 * time.Second, End: 4 * time.Second},
		{Word: "الحمد", Start: 4 * time.Second, End: 5 * time.Second},
		{Word: "لله", Start: 5 * time.Second, End: 6 * time.Second},
		{Word: "رب", Start: 6 * time.Second, End: 7 * time.Second},
		{Word: "العالمين", Start: 7 * time.Second, End: 8 * time.Second},
		{Word: "اياك", Start: 8 * time.Second, End: 9 * time.Second},
		{Word: "نعبد", Start: 9 * time.Second, End: 10 * time.Second},
		{Word: "واياك", Start: 10 * time.Second, End: 11 * time.Second},
		{Word: "نستعين", Start: 11 * time.Second, End: 12 * time.Second},
		{Word: "اهدنا", Start: 12 * time.Second, End: 13 * time.Second},
		{Word: "الصراط", Start: 13 * time.Second, End: 14 * time.Second},
		{Word: "المستقيم", Start: 14 * time.Second, End: 15 * time.Second},
		{Word: "امين", Start: 15 * time.Second, End: 16 * time.Second},
		{Word: "الم", Start: 19 * time.Second, End: 20 * time.Second},
	}
	cut, summary, ok, err := detectLeadingFatihaCut(context.Background(), words, corpus)
	if err != nil {
		t.Fatalf("detectLeadingFatihaCut failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected fatiha to be detected")
	}
	if cut != 19*time.Second {
		t.Fatalf("expected cut to start at next recitation word after ameen, got %s", cut)
	}
	if summary.matches <= 0 || summary.coverage <= 0 {
		t.Fatalf("expected positive summary, got %+v", summary)
	}
}

func TestDetectLeadingFatihaCut_NoMatch(t *testing.T) {
	corpus := &fakeCorpus{
		surah: map[int][]recognize.Ayah{
			1: {
				{NumberInSurah: 1, Text: "بِسْمِ اللَّهِ الرَّحْمَٰنِ الرَّحِيمِ"},
				{NumberInSurah: 2, Text: "الْحَمْدُ لِلَّهِ رَبِّ الْعَالَمِينَ"},
				{NumberInSurah: 3, Text: "الرَّحْمَٰنِ الرَّحِيمِ"},
				{NumberInSurah: 4, Text: "مَالِكِ يَوْمِ الدِّينِ"},
				{NumberInSurah: 5, Text: "إِيَّاكَ نَعْبُدُ وَإِيَّاكَ نَسْتَعِينُ"},
				{NumberInSurah: 6, Text: "اهْدِنَا الصِّرَاطَ الْمُسْتَقِيمَ"},
				{NumberInSurah: 7, Text: "صِرَاطَ الَّذِينَ أَنْعَمْتَ عَلَيْهِمْ"},
			},
		},
	}
	words := []align.WordTiming{
		{Word: "الم", Start: 0 * time.Second, End: 1 * time.Second},
		{Word: "ذلك", Start: 1 * time.Second, End: 2 * time.Second},
		{Word: "الكتاب", Start: 2 * time.Second, End: 3 * time.Second},
		{Word: "لا", Start: 3 * time.Second, End: 4 * time.Second},
		{Word: "ريب", Start: 4 * time.Second, End: 5 * time.Second},
		{Word: "فيه", Start: 5 * time.Second, End: 6 * time.Second},
	}
	_, _, ok, err := detectLeadingFatihaCut(context.Background(), words, corpus)
	if err != nil {
		t.Fatalf("detectLeadingFatihaCut failed: %v", err)
	}
	if ok {
		t.Fatalf("did not expect fatihah detection")
	}
}

func TestIsCaptionsOnlyMode(t *testing.T) {
	if !isCaptionsOnlyMode("captions") {
		t.Fatalf("expected captions mode to be recognized")
	}
	if !isCaptionsOnlyMode("subtitle") {
		t.Fatalf("expected subtitle mode to be recognized")
	}
	if isCaptionsOnlyMode("sequential") {
		t.Fatalf("did not expect sequential to be captions-only mode")
	}
}

func TestForceSRTExt(t *testing.T) {
	if got := forceSRTExt("out/video.mp4"); got != "out/video.srt" {
		t.Fatalf("unexpected converted path: %s", got)
	}
	if got := forceSRTExt("out/caption.srt"); got != "out/caption.srt" {
		t.Fatalf("expected unchanged srt path, got: %s", got)
	}
}

func TestLiveChunkFilesExcludesDerivedCleanAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"chunk_00000.wav", "chunk_00000.wav.clean.wav", "chunk_00001.wav"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("write chunk fixture: %v", err)
		}
	}
	files := liveChunkFiles(dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 source chunks, got %v", files)
	}
}

func TestLiveStreamInputArgsLimitsInputDuration(t *testing.T) {
	args, err := liveStreamInputArgs("https://example.com/live.m3u8", 45*time.Second)
	if err != nil {
		t.Fatalf("liveStreamInputArgs failed: %v", err)
	}
	if got := strings.Join(args, " "); !strings.Contains(got, "-t 45.000 -i https://example.com/live.m3u8") {
		t.Fatalf("expected duration before input, got %q", got)
	}
}

func TestRepairCompressedVerseWordTimings(t *testing.T) {
	timings := []render.Timing{
		{
			WordTimings: []render.WordTiming{
				{Word: "a", Start: 10 * time.Second, End: 10*time.Second + 100*time.Millisecond},
				{Word: "b", Start: 10*time.Second + 100*time.Millisecond, End: 10*time.Second + 200*time.Millisecond},
				{Word: "c", Start: 10*time.Second + 200*time.Millisecond, End: 10*time.Second + 300*time.Millisecond},
			},
		},
		{
			WordTimings: []render.WordTiming{{Word: "d", Start: 12 * time.Second, End: 13 * time.Second}},
		},
	}

	repairCompressedVerseWordTimings(timings)

	words := timings[0].WordTimings
	if words[0].Start != 10*time.Second || words[len(words)-1].End != 12*time.Second {
		t.Fatalf("expected compressed verse to fill 10s-12s, got %s-%s", words[0].Start, words[len(words)-1].End)
	}
	if timings[0].End != 12*time.Second {
		t.Fatalf("expected verse end to match following ayah, got %s", timings[0].End)
	}
}

func TestApplyAyahBoundariesBeforeNormalizingWordTimings(t *testing.T) {
	timings := []render.Timing{
		{
			Start: 0,
			End:   31 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "previous", Start: 30 * time.Second, End: 31 * time.Second},
			},
		},
		{
			Start: 38 * time.Second,
			End:   45 * time.Second,
			WordTimings: []render.WordTiming{
				{Word: "وَيُحِقُّ", Start: 31 * time.Second, End: 32 * time.Second},
				{Word: "اللَّهُ", Start: 32 * time.Second, End: 33 * time.Second},
			},
		},
	}

	applyAyahBoundariesFromWordTimings(timings)
	normalizeWordTimings(timings)

	if got := timings[1].WordTimings[0].Start; got != 31*time.Second {
		t.Fatalf("expected Whisper timestamp to remain at 31s, got %s", got)
	}
}
