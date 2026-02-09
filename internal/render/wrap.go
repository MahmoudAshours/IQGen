package render

import (
	"strings"
	"unicode"

	"qgencodex/internal/config"
)

const (
	wrapThreshold = 0.98
	avgCharWidth  = 0.60
	avgSpaceWidth = 0.33
)

func wrapText(text string, maxWidth int, fontSize int) []string {
	clean := sanitizeText(text)
	if clean == "" || maxWidth <= 0 || fontSize <= 0 {
		return []string{clean}
	}
	threshold := float64(maxWidth) * wrapThreshold
	words := strings.Fields(clean)
	if len(words) == 0 {
		return []string{clean}
	}
	var lines []string
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if approxWidth(candidate, fontSize) > threshold {
			if current == "" {
				parts := splitLongWord(word, threshold, fontSize)
				lines = append(lines, parts...)
				current = ""
				continue
			}
			lines = append(lines, current)
			current = word
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func maybeElongateLines(cfg config.VideoConfig, lines []string, maxWidth int, fontSize int) []string {
	if !cfg.Elongate || len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	target := float64(maxWidth) * 0.90
	for _, line := range lines {
		out = append(out, elongateLine(line, target, fontSize))
	}
	return out
}

func elongateText(text string) string {
	if strings.Contains(text, "_") {
		return strings.ReplaceAll(text, "_", "ـ")
	}
	return text
}

func elongateLine(line string, target float64, fontSize int) string {
	if line == "" || target <= 0 || fontSize <= 0 {
		return line
	}
	if strings.Contains(line, "_") {
		return elongateText(line)
	}
	if approxWidth(line, fontSize) >= target {
		return line
	}
	runes := []rune(line)
	positions := elongationPositions(runes)
	if len(positions) == 0 {
		return line
	}
	maxInsert := 32
	posIdx := 0
	for i := 0; i < maxInsert && approxWidth(string(runes), fontSize) < target; i++ {
		insertAt := positions[posIdx]
		runes = insertRune(runes, insertAt, 'ـ')
		for j := range positions {
			if positions[j] >= insertAt {
				positions[j]++
			}
		}
		posIdx++
		if posIdx >= len(positions) {
			posIdx = 0
		}
	}
	return string(runes)
}

func elongationPositions(runes []rune) []int {
	var positions []int
	for i := 0; i < len(runes); i++ {
		if !isArabicLetter(runes[i]) {
			continue
		}
		j := i + 1
		for j < len(runes) && isCombining(runes[j]) {
			j++
		}
		if j >= len(runes) {
			continue
		}
		if !isArabicLetter(runes[j]) {
			continue
		}
		positions = append(positions, j)
	}
	return positions
}

func insertRune(runes []rune, idx int, r rune) []rune {
	if idx < 0 || idx > len(runes) {
		return runes
	}
	runes = append(runes, 0)
	copy(runes[idx+1:], runes[idx:])
	runes[idx] = r
	return runes
}

func isArabicLetter(r rune) bool {
	return (r >= 0x0600 && r <= 0x06FF) || (r >= 0x0750 && r <= 0x077F) || (r >= 0x08A0 && r <= 0x08FF)
}

func splitLongWord(word string, threshold float64, fontSize int) []string {
	var lines []string
	var current strings.Builder
	for _, r := range word {
		if isCombining(r) {
			current.WriteRune(r)
			continue
		}
		next := current.String() + string(r)
		if approxWidth(next, fontSize) > threshold && current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func approxWidth(text string, fontSize int) float64 {
	var chars int
	var spaces int
	for _, r := range text {
		if unicode.IsSpace(r) {
			spaces++
			continue
		}
		if isCombining(r) {
			continue
		}
		chars++
	}
	return float64(chars)*float64(fontSize)*avgCharWidth + float64(spaces)*float64(fontSize)*avgSpaceWidth
}

func isCombining(r rune) bool {
	return unicode.Is(unicode.Mn, r)
}

func sanitizeText(text string) string {
	return strings.TrimPrefix(text, "\ufeff")
}

func maxTextWidth(cfg config.VideoConfig, width int) int {
	left := cfg.Margins.Left
	right := cfg.Margins.Right
	if left == 0 {
		left = 120
	}
	if right == 0 {
		right = 120
	}
	maxWidth := width - left - right
	if maxWidth <= 0 {
		maxWidth = int(float64(width) * 0.9)
	}
	return maxWidth
}
