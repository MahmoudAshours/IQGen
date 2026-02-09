package recognize

import (
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
	Cmd string
}

func NewWhisperRecognizer(cmd string) *WhisperRecognizer {
	if cmd == "" {
		cmd = "whisper"
	}
	return &WhisperRecognizer{Cmd: cmd}
}

func (w *WhisperRecognizer) Available() bool {
	_, err := exec.LookPath(w.Cmd)
	return err == nil
}

func (w *WhisperRecognizer) Transcribe(ctx context.Context, audioPath string, language string) (string, error) {
	if !w.Available() {
		return "", fmt.Errorf("whisper command not found: %s", w.Cmd)
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

type whisperTranscript struct {
	Text string `json:"text"`
}

func (w *WhisperRecognizer) Identify(ctx context.Context, audioPath string, language string, matcher *Matcher) (Result, string, error) {
	if matcher == nil {
		return Result{}, "", errors.New("matcher is nil")
	}
	transcript, err := w.Transcribe(ctx, audioPath, language)
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
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
