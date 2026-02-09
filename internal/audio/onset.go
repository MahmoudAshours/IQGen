package audio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sort"
	"time"
)

// EstimateWordOffset estimates a global timing offset based on energy onsets near word starts.
func EstimateWordOffset(ctx context.Context, audioPath string, wordStarts []time.Duration, windowMs int) (time.Duration, error) {
	if len(wordStarts) == 0 {
		return 0, errors.New("no word starts provided")
	}
	if windowMs <= 0 {
		windowMs = 80
	}
	const sampleRate = 16000
	const frameMs = 10
	frameSamples := sampleRate * frameMs / 1000
	if frameSamples <= 0 {
		return 0, errors.New("invalid frame size")
	}
	frameBytes := frameSamples * 2

	energies, err := readEnergies(ctx, audioPath, sampleRate, frameBytes, frameSamples)
	if err != nil {
		return 0, err
	}
	if len(energies) == 0 {
		return 0, errors.New("no energy frames extracted")
	}

	frameDur := time.Duration(frameMs) * time.Millisecond
	windowFrames := windowMs / frameMs
	if windowFrames < 1 {
		windowFrames = 1
	}

	starts := sampleStarts(wordStarts, 80)
	offsets := make([]time.Duration, 0, len(starts))
	maxOffset := time.Duration(windowMs) * time.Millisecond
	for _, start := range starts {
		center := int(start / frameDur)
		idx, ok := estimateOnsetIdx(energies, center, windowFrames)
		if !ok {
			continue
		}
		onset := time.Duration(idx) * frameDur
		delta := onset - start
		if delta > maxOffset || delta < -maxOffset {
			continue
		}
		offsets = append(offsets, delta)
	}
	if len(offsets) == 0 {
		return 0, errors.New("no valid offsets found")
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	median := offsets[len(offsets)/2]
	return median, nil
}

func readEnergies(ctx context.Context, audioPath string, sampleRate int, frameBytes int, frameSamples int) ([]float64, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", audioPath,
		"-vn",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-f", "s16le",
		"-",
	)
	var stderr bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(stdout)
	frame := make([]byte, frameBytes)
	energies := make([]float64, 0, 4096)
	for {
		n, err := io.ReadFull(reader, frame)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			_ = cmd.Wait()
			return nil, fmt.Errorf("ffmpeg read failed: %w", err)
		}
		if n < frameBytes {
			break
		}
		sum := 0.0
		for i := 0; i < frameSamples; i++ {
			offset := i * 2
			sample := int16(binary.LittleEndian.Uint16(frame[offset:]))
			v := float64(sample)
			sum += v * v
		}
		rms := math.Sqrt(sum / float64(frameSamples))
		energies = append(energies, rms)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return energies, nil
}

func estimateOnsetIdx(energies []float64, center int, window int) (int, bool) {
	if len(energies) == 0 {
		return 0, false
	}
	if center < 0 {
		center = 0
	}
	if center >= len(energies) {
		center = len(energies) - 1
	}
	start := center - window
	if start < 0 {
		start = 0
	}
	end := center + window
	if end >= len(energies) {
		end = len(energies) - 1
	}
	if end-start < 2 {
		return center, true
	}
	minE := energies[start]
	maxE := energies[start]
	for i := start + 1; i <= end; i++ {
		v := energies[i]
		if v < minE {
			minE = v
		}
		if v > maxE {
			maxE = v
		}
	}
	if maxE <= minE {
		return center, false
	}
	threshold := minE + (maxE-minE)*0.35
	for i := start + 1; i <= end; i++ {
		if energies[i-1] < threshold && energies[i] >= threshold {
			return i, true
		}
	}
	bestIdx := center
	bestDelta := 0.0
	for i := start + 1; i <= end; i++ {
		delta := energies[i] - energies[i-1]
		if delta > bestDelta {
			bestDelta = delta
			bestIdx = i
		}
	}
	if bestDelta > 0 {
		return bestIdx, true
	}
	return center, false
}

func sampleStarts(values []time.Duration, max int) []time.Duration {
	if len(values) <= max || max <= 0 {
		return values
	}
	step := len(values) / max
	if step < 1 {
		step = 1
	}
	out := make([]time.Duration, 0, max)
	for i := 0; i < len(values); i += step {
		out = append(out, values[i])
		if len(out) >= max {
			break
		}
	}
	return out
}
