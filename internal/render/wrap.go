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
		out = append(out, elongateLine(line, target, fontSize, cfg.ElongateCount))
	}
	return out
}

func elongateText(text string, count int) string {
	if strings.Contains(text, "_") {
		return applyKashidaMarks(text, count)
	}
	return text
}

func elongateLine(line string, target float64, fontSize int, count int) string {
	if line == "" || target <= 0 || fontSize <= 0 {
		return line
	}
	if strings.Contains(line, "_") {
		return elongateText(line, count)
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
		runes = insertRunes(runes, insertAt, 'ـ', normalizedElongateCount(count))
		for j := range positions {
			if positions[j] >= insertAt {
				positions[j] += normalizedElongateCount(count)
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
		if !isKashidaEligible(runes[i]) {
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

func insertRunes(runes []rune, idx int, r rune, count int) []rune {
	if count <= 0 {
		return runes
	}
	if count == 1 {
		return insertRune(runes, idx, r)
	}
	if idx < 0 || idx > len(runes) {
		idx = len(runes)
	}
	extra := make([]rune, count)
	for i := range extra {
		extra[i] = r
	}
	runes = append(runes, extra...)
	copy(runes[idx+count:], runes[idx:])
	copy(runes[idx:], extra)
	return runes
}

func isArabicLetter(r rune) bool {
	return (r >= 0x0600 && r <= 0x06FF) || (r >= 0x0750 && r <= 0x077F) || (r >= 0x08A0 && r <= 0x08FF)
}

func isKashidaEligible(r rune) bool {
	switch r {
	case 'ا', 'أ', 'إ', 'آ', 'د', 'ذ', 'ر', 'ز', 'و', 'ؤ', 'ء', 'ى', 'ة':
		return false
	default:
		return true
	}
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

func normalizedElongateCount(count int) int {
	if count <= 0 {
		return 1
	}
	return count
}

func applyKashidaMarks(text string, count int) string {
	if !strings.Contains(text, "_") {
		return text
	}
	kCount := normalizedElongateCount(count)
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	lastArabic := -1
	lastEligible := false
	pending := 0
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == '_' {
			run := 0
			for i < len(runes) && runes[i] == '_' {
				run++
				i++
			}
			if lastArabic >= 0 && lastEligible {
				insertAt := lastArabic + 1
				for insertAt < len(out) && isCombining(out[insertAt]) {
					insertAt++
				}
				out = insertRunes(out, insertAt, 'ـ', run*kCount)
			} else {
				pending += run * kCount
			}
			continue
		}
		if pending > 0 && isArabicLetter(r) {
			out = append(out, r)
			lastArabic = len(out) - 1
			lastEligible = isKashidaEligible(r)
			j := i + 1
			for j < len(runes) && isCombining(runes[j]) {
				out = append(out, runes[j])
				j++
			}
			if lastEligible {
				out = insertRunes(out, len(out), 'ـ', pending)
				pending = 0
			}
			i = j
			continue
		}
		if pending > 0 && unicode.IsSpace(r) {
			pending = 0
		}
		out = append(out, r)
		if isArabicLetter(r) {
			lastArabic = len(out) - 1
			lastEligible = isKashidaEligible(r)
		}
		i++
	}
	return string(out)
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
