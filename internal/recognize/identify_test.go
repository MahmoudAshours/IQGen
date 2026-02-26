package recognize

import (
	"errors"
	"testing"
)

func TestUseGoWhisperDetection(t *testing.T) {
	r := NewWhisperRecognizerWithOptions(WhisperOptions{Command: "gowhisper", Backend: "auto"})
	if !r.useGoWhisper() {
		t.Fatalf("expected go-whisper backend")
	}
	r = NewWhisperRecognizerWithOptions(WhisperOptions{Command: "whisper", Backend: "python"})
	if r.useGoWhisper() {
		t.Fatalf("expected python backend")
	}
	r = NewWhisperRecognizerWithOptions(WhisperOptions{Command: "whisper", Backend: "gowhisper"})
	if !r.useGoWhisper() {
		t.Fatalf("expected forced go-whisper backend")
	}
}

func TestRecognizerIdentifyModelOption(t *testing.T) {
	r := NewWhisperRecognizerWithOptions(WhisperOptions{
		Command:                "gowhisper",
		Backend:                "gowhisper",
		GoWhisperModel:         "ggml-medium-q5_0",
		GoWhisperIdentifyModel: "ggml-small-q5_0",
	})
	if r.GoWhisperIdentifyModel != "ggml-small-q5_0" {
		t.Fatalf("expected identify model to be set")
	}
	if r.GoWhisperModel != "ggml-medium-q5_0" {
		t.Fatalf("expected primary model to be set")
	}
}

func TestParseGoWhisperTranscriptFromSegments(t *testing.T) {
	data := []byte(`{"segments":[{"text":"بسم الله"},{"words":[{"word":"الرحمن"},{"text":"الرحيم"}]}]}`)
	text, err := parseGoWhisperTranscript(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "بسم الله الرحمن الرحيم" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestExtractFirstJSON_Array(t *testing.T) {
	data := []byte("debug\n[{\"segments\":[{\"text\":\"abc\"}]}]\n")
	blob, err := extractFirstJSON(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(blob) != "[{\"segments\":[{\"text\":\"abc\"}]}]" {
		t.Fatalf("unexpected blob: %s", string(blob))
	}
}

func TestGoWhisperModelCandidates(t *testing.T) {
	r := NewWhisperRecognizerWithOptions(WhisperOptions{
		Command:                "gowhisper",
		Backend:                "gowhisper",
		GoWhisperModel:         "ggml-medium-q5_0",
		GoWhisperIdentifyModel: "ggml-small-q5_0",
	})
	got := r.goWhisperModelCandidates(true)
	if len(got) != 2 || got[0] != "ggml-small-q5_0" || got[1] != "ggml-medium-q5_0" {
		t.Fatalf("unexpected candidates: %#v", got)
	}
	got = r.goWhisperModelCandidates(false)
	if len(got) != 1 || got[0] != "ggml-medium-q5_0" {
		t.Fatalf("unexpected non-identify candidates: %#v", got)
	}
}

func TestIsGoWhisperModelSelectionError(t *testing.T) {
	if !isGoWhisperModelSelectionError(errors.New(`transcribe error: "Not Found: ggml-small-q5_0"`)) {
		t.Fatalf("expected not-found to be classified as model selection error")
	}
	if isGoWhisperModelSelectionError(errors.New("context deadline exceeded")) {
		t.Fatalf("did not expect timeout to be classified as model selection error")
	}
}
