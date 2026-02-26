package recognize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	Surah     int
	StartAyah int
	EndAyah   int
}

type WhisperRecognizer struct {
	Cmd                    string
	Backend                string
	GoWhisperModel         string
	GoWhisperIdentifyModel string
	GoWhisperRemote        bool
	GoWhisperAddr          string
}

func NewWhisperRecognizer(cmd string) *WhisperRecognizer {
	return NewWhisperRecognizerWithOptions(WhisperOptions{Command: cmd})
}

type WhisperOptions struct {
	Command                string
	Backend                string
	GoWhisperModel         string
	GoWhisperIdentifyModel string
	GoWhisperRemote        bool
	GoWhisperAddr          string
}

func NewWhisperRecognizerWithOptions(opts WhisperOptions) *WhisperRecognizer {
	cmd := strings.TrimSpace(opts.Command)
	if cmd == "" {
		cmd = "whisper"
	}
	backend := strings.ToLower(strings.TrimSpace(opts.Backend))
	if backend == "" {
		backend = "auto"
	}
	model := strings.TrimSpace(opts.GoWhisperModel)
	if model == "" {
		model = "ggml-medium-q5_0"
	}
	return &WhisperRecognizer{
		Cmd:                    cmd,
		Backend:                backend,
		GoWhisperModel:         model,
		GoWhisperIdentifyModel: strings.TrimSpace(opts.GoWhisperIdentifyModel),
		GoWhisperRemote:        opts.GoWhisperRemote,
		GoWhisperAddr:          strings.TrimSpace(opts.GoWhisperAddr),
	}
}

func (w *WhisperRecognizer) Available() bool {
	_, err := exec.LookPath(w.Cmd)
	return err == nil
}

func (w *WhisperRecognizer) Transcribe(ctx context.Context, audioPath string, language string) (string, error) {
	return w.transcribe(ctx, audioPath, language, false)
}

func (w *WhisperRecognizer) transcribe(ctx context.Context, audioPath string, language string, forIdentify bool) (string, error) {
	if !w.Available() {
		return "", fmt.Errorf("speech-to-text command not found: %s", w.Cmd)
	}
	if w.useGoWhisper() {
		return w.transcribeGoWhisper(ctx, audioPath, language, forIdentify)
	}
	outputDir, err := os.MkdirTemp("", "quranvideo-identify-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(outputDir)

	if language == "" {
		language = "ar"
	}
	jsonPath := filepath.Join(outputDir, strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))+".json")
	baseArgs := []string{
		"--language", language,
		"--output_format", "json",
		"--output_dir", outputDir,
		"--task", "transcribe",
		audioPath,
	}
	advancedArgs := append([]string{}, baseArgs...)
	advancedArgs = append(advancedArgs,
		"--temperature", "0",
		"--beam_size", "5",
		"--best_of", "5",
	)
	if err := runWhisper(ctx, w.Cmd, advancedArgs); err != nil {
		_ = os.Remove(jsonPath)
		if err := runWhisper(ctx, w.Cmd, baseArgs); err != nil {
			return "", err
		}
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return "", err
	}
	var result whisperTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return "", errors.New("empty transcription")
	}
	return text, nil
}

func (w *WhisperRecognizer) useGoWhisper() bool {
	switch strings.ToLower(strings.TrimSpace(w.Backend)) {
	case "gowhisper", "go-whisper":
		return true
	case "python", "whisper":
		return false
	}
	base := strings.ToLower(filepath.Base(w.Cmd))
	return strings.Contains(base, "gowhisper") || strings.Contains(base, "go-whisper")
}

func (w *WhisperRecognizer) transcribeGoWhisper(ctx context.Context, audioPath, language string, forIdentify bool) (string, error) {
	models := w.goWhisperModelCandidates(forIdentify)
	var lastErr error
	for idx, model := range models {
		args := []string{"transcribe", model, audioPath, "--format", "json"}
		if language != "" {
			args = append(args, "--language", language)
		}
		if w.GoWhisperRemote {
			args = append(args, "--remote")
		}
		data, err := runGoWhisper(ctx, w.Cmd, args, w.GoWhisperAddr)
		if err != nil {
			lastErr = err
			if idx < len(models)-1 && isGoWhisperModelSelectionError(err) {
				continue
			}
			return "", err
		}
		text, err := parseGoWhisperTranscript(data)
		if err != nil {
			lastErr = err
			return "", err
		}
		return text, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("go-whisper model not configured")
}

func (w *WhisperRecognizer) goWhisperModelCandidates(forIdentify bool) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if forIdentify {
		add(w.GoWhisperIdentifyModel)
	}
	add(w.GoWhisperModel)
	if len(out) == 0 {
		add("ggml-medium-q5_0")
	}
	return out
}

func isGoWhisperModelSelectionError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") ||
		strings.Contains(s, "unknown model") ||
		strings.Contains(s, "bad file extension")
}

type whisperTranscript struct {
	Text string `json:"text"`
}

type whisperWordPart struct {
	Word string `json:"word"`
	Text string `json:"text"`
}

type whisperSegmentPart struct {
	Text  string            `json:"text"`
	Words []whisperWordPart `json:"words"`
}

type whisperSegmentContainer struct {
	Text     string               `json:"text"`
	Segments []whisperSegmentPart `json:"segments"`
}

func parseGoWhisperTranscript(data []byte) (string, error) {
	var result whisperTranscript
	if err := json.Unmarshal(data, &result); err == nil {
		if text := strings.TrimSpace(result.Text); text != "" {
			return text, nil
		}
	}
	var container whisperSegmentContainer
	if err := json.Unmarshal(data, &container); err == nil {
		if text := strings.TrimSpace(container.Text); text != "" {
			return text, nil
		}
		joined := strings.TrimSpace(joinWhisperSegments(container.Segments))
		if joined != "" {
			return joined, nil
		}
	}
	var segments []whisperSegmentPart
	if err := json.Unmarshal(data, &segments); err == nil {
		joined := strings.TrimSpace(joinWhisperSegments(segments))
		if joined != "" {
			return joined, nil
		}
	}
	var containers []whisperSegmentContainer
	if err := json.Unmarshal(data, &containers); err == nil {
		var parts []string
		for _, c := range containers {
			if text := strings.TrimSpace(c.Text); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := strings.TrimSpace(joinWhisperSegments(c.Segments)); text != "" {
				parts = append(parts, text)
			}
		}
		joined := strings.TrimSpace(strings.Join(parts, " "))
		if joined != "" {
			return joined, nil
		}
	}
	return "", errors.New("empty transcription")
}

func joinWhisperSegments(segments []whisperSegmentPart) string {
	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if text := strings.TrimSpace(seg.Text); text != "" {
			parts = append(parts, text)
			continue
		}
		for _, w := range seg.Words {
			word := strings.TrimSpace(w.Word)
			if word == "" {
				word = strings.TrimSpace(w.Text)
			}
			if word != "" {
				parts = append(parts, word)
			}
		}
	}
	return strings.Join(parts, " ")
}

func (w *WhisperRecognizer) Identify(ctx context.Context, audioPath string, language string, matcher *Matcher) (Result, string, error) {
	if matcher == nil {
		return Result{}, "", errors.New("matcher is nil")
	}
	transcript, err := w.transcribe(ctx, audioPath, language, true)
	if err != nil {
		return Result{}, "", err
	}
	result, err := matcher.Identify(ctx, transcript)
	if err != nil {
		return Result{}, transcript, err
	}
	return result, transcript, nil
}

func runWhisper(ctx context.Context, cmd string, args []string) error {
	c := exec.CommandContext(ctx, cmd, args...)
	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("whisper failed: %w: %s", err, truncateErrorOutput(output.String(), 600))
	}
	return nil
}

func runGoWhisper(ctx context.Context, cmd string, args []string, addr string) ([]byte, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	if strings.TrimSpace(addr) != "" {
		c.Env = append(os.Environ(), "GOWHISPER_ADDR="+strings.TrimSpace(addr))
	}
	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(output.String())
		if strings.Contains(strings.ToLower(msg), "connection refused") {
			return nil, fmt.Errorf("go-whisper server unavailable (%s). Start go-whisper server or set audio.go_whisper_addr correctly", msg)
		}
		return nil, fmt.Errorf("go-whisper failed: %w: %s", err, truncateErrorOutput(msg, 600))
	}
	blob, err := extractFirstJSON(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("go-whisper output does not contain JSON: %w", err)
	}
	return blob, nil
}

func extractFirstJSON(raw []byte) ([]byte, error) {
	data := bytes.TrimSpace(raw)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty output")
	}
	if json.Valid(data) {
		return data, nil
	}
	for i, b := range data {
		if b != '{' && b != '[' {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(data[i:]))
		var blob json.RawMessage
		if err := dec.Decode(&blob); err != nil {
			continue
		}
		blob = bytes.TrimSpace(blob)
		if len(blob) == 0 {
			continue
		}
		if json.Valid(blob) {
			return blob, nil
		}
	}
	return nil, fmt.Errorf("could not parse JSON from output: %s", truncateErrorOutput(string(data), 600))
}

func truncateErrorOutput(s string, max int) string {
	t := strings.TrimSpace(s)
	if max <= 0 || len(t) <= max {
		return t
	}
	return t[:max] + "...(truncated)"
}
