package align

import (
	"testing"
	"time"
)

func TestParseGoWhisperTime(t *testing.T) {
	d, err := parseGoWhisperTime("00:00:01.500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 1500*time.Millisecond {
		t.Fatalf("unexpected duration: %v", d)
	}
	d, err = parseGoWhisperTime("2.25")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 2250*time.Millisecond {
		t.Fatalf("unexpected numeric duration: %v", d)
	}
}

func TestParseGoWhisperFlatWords(t *testing.T) {
	input := []byte(`{
		"text":"الحمد لله رب العالمين",
		"segments":[
			{"start":"00:00:01.000","end":"00:00:03.000","text":"الحمد لله"},
			{"start":3.0,"end":4.2,"words":[
				{"word":"رب","start":"00:00:03.100","end":"00:00:03.500"},
				{"text":"العالمين","start":3.5,"end":4.2}
			]}
		]
	}`)
	words, err := parseGoWhisperFlatWords(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 4 {
		t.Fatalf("expected 4 words, got %d", len(words))
	}
	if words[0].Word != "الحمد" || words[1].Word != "لله" || words[2].Word != "رب" || words[3].Word != "العالمين" {
		t.Fatalf("unexpected words: %#v", words)
	}
}

func TestParseGoWhisperFlatWords_ArrayShape(t *testing.T) {
	input := []byte(`[
		{"start":"00:00:00.000","end":"00:00:01.000","text":"الحمد لله"},
		{"start":"00:00:01.000","end":"00:00:02.000","words":[
			{"word":"رب","start":"00:00:01.000","end":"00:00:01.400"},
			{"word":"العالمين","start":"00:00:01.400","end":"00:00:02.000"}
		]}
	]`)
	words, err := parseGoWhisperFlatWords(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 4 {
		t.Fatalf("expected 4 words, got %d", len(words))
	}
}

func TestExtractFirstJSON(t *testing.T) {
	input := []byte("log line\n[{\"text\":\"x\"}]\ntrailing log")
	blob, err := extractFirstJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(blob) != "[{\"text\":\"x\"}]" {
		t.Fatalf("unexpected blob: %s", string(blob))
	}
}

func TestUseGoWhisperDetection(t *testing.T) {
	w := NewWhisperAlignerWithOptions(Options{Command: "gowhisper", Backend: "auto"})
	if !w.useGoWhisper() {
		t.Fatalf("expected gowhisper command to select go-whisper backend")
	}
	w = NewWhisperAlignerWithOptions(Options{Command: "whisper", Backend: "python"})
	if w.useGoWhisper() {
		t.Fatalf("expected python backend to disable go-whisper")
	}
}

func TestAlignWordsToTranscript(t *testing.T) {
	words := []string{"بسم", "الله", "الرحمن"}
	whisperWords := []WordTiming{
		{Word: "بسم", Start: 0, End: 400 * time.Millisecond},
		{Word: "الله", Start: 400 * time.Millisecond, End: 900 * time.Millisecond},
		{Word: "الرحمن", Start: 900 * time.Millisecond, End: 1400 * time.Millisecond},
	}
	aligned, err := AlignWordsToTranscript(words, whisperWords)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(aligned) != len(words) {
		t.Fatalf("expected %d words, got %d", len(words), len(aligned))
	}
	for i := range aligned {
		if aligned[i].Word != words[i] {
			t.Fatalf("unexpected word at %d: %q", i, aligned[i].Word)
		}
		if aligned[i].End < aligned[i].Start {
			t.Fatalf("invalid timing at %d: %v-%v", i, aligned[i].Start, aligned[i].End)
		}
	}
}
